package streamer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Push error constants
var (
	ErrPushContextNil  = errors.New("context cannot be nil")
	ErrPushBucketEmpty = errors.New("bucket cannot be empty")
	ErrPushKeyEmpty    = errors.New("key cannot be empty")
	ErrPushDirsEmpty   = errors.New("dirs cannot be empty")
	ErrPushUploaderNil = errors.New("uploader cannot be nil")
)

// Push streams directories to R2 as tar.gz, optionally saving local copy
func Push(ctx context.Context, cfg PushConfig, uploader S3StreamUploader) (Result, error) {
	if ctx == nil {
		return Result{}, ErrPushContextNil
	}
	if cfg.Bucket == "" {
		return Result{}, ErrPushBucketEmpty
	}
	if cfg.Key == "" {
		return Result{}, ErrPushKeyEmpty
	}
	if len(cfg.Dirs) == 0 {
		return Result{}, ErrPushDirsEmpty
	}
	if uploader == nil {
		return Result{}, ErrPushUploaderNil
	}

	// Evaluate local backup condition BEFORE streaming starts
	doLocalBackup := cfg.LocalPath != "" && (cfg.ShouldBackup == nil || cfg.ShouldBackup())

	// Create pipe for streaming
	pipeReader, pipeWriter := io.Pipe()

	// Result tracking
	var result Result
	result.Key = cfg.Key

	// Hash writer for checksum
	hashWriter := sha256.New()

	// Counting writer to track size
	var bytesWritten int64
	countWriter := &countingWriter{w: hashWriter, n: &bytesWritten}

	var producerErr error
	var consumerErr error
	var wg sync.WaitGroup

	// Producer goroutine: walks dirs -> tar -> gzip -> pipe
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				producerErr = fmt.Errorf("producer panic: %v", r)
				pipeWriter.CloseWithError(producerErr)
			}
		}()

		producerErr = runProducer(ctx, cfg.Dirs, countWriter, pipeWriter)
		if producerErr != nil {
			pipeWriter.CloseWithError(producerErr)
		}
	}()

	// Consumer goroutine: pipe -> [TeeReader -> local] -> S3 upload
	wg.Add(1)
	go func() {
		defer wg.Done()
		var localPath string
		localPath, consumerErr = runConsumer(ctx, cfg, pipeReader, uploader, doLocalBackup)
		if consumerErr == nil {
			result.LocalPath = localPath
		}
	}()

	// Wait for both goroutines
	wg.Wait()

	// Check for errors (producer error takes precedence)
	if producerErr != nil {
		return Result{}, producerErr
	}
	if consumerErr != nil {
		return Result{}, consumerErr
	}

	result.Size = bytesWritten
	result.Checksum = fmt.Sprintf("%x", hashWriter.Sum(nil))

	return result, nil
}

// runProducer handles the producer goroutine logic
func runProducer(ctx context.Context, dirs []string, countWriter io.Writer, pipeWriter *io.PipeWriter) error {
	if ctx == nil {
		return ErrPushContextNil
	}
	if pipeWriter == nil {
		return errors.New("pipeWriter cannot be nil")
	}

	// Chain: tarWriter -> gzipWriter -> multiWriter(countWriter, pipeWriter)
	multiW := io.MultiWriter(countWriter, pipeWriter)
	gzipWriter, err := gzip.NewWriterLevel(multiW, gzip.BestSpeed)
	if err != nil {
		pipeWriter.Close()
		return fmt.Errorf("failed to create gzip writer: %w", err)
	}

	tarWriter := tar.NewWriter(gzipWriter)

	// Walk all directories and write to tar
	for _, dir := range dirs {
		if err := addDirToTar(ctx, tarWriter, dir); err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			pipeWriter.Close()
			return fmt.Errorf("failed to add %s to tar: %w", dir, err)
		}
	}

	// CRITICAL: Close order matters
	if err := tarWriter.Close(); err != nil {
		gzipWriter.Close()
		pipeWriter.Close()
		return fmt.Errorf("failed to close tar writer: %w", err)
	}

	if err := gzipWriter.Close(); err != nil {
		pipeWriter.Close()
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	pipeWriter.Close()
	return nil
}

// runConsumer handles the consumer goroutine logic
func runConsumer(ctx context.Context, cfg PushConfig, pipeReader *io.PipeReader, uploader S3StreamUploader, doLocalBackup bool) (string, error) {
	if ctx == nil {
		return "", ErrPushContextNil
	}
	if pipeReader == nil {
		return "", errors.New("pipeReader cannot be nil")
	}

	var uploadReader io.Reader = pipeReader
	var localFile *os.File
	var localPath string

	// Setup local backup via TeeReader if needed
	if doLocalBackup {
		var err error
		localFile, err = setupLocalBackup(cfg.LocalPath)
		if err != nil {
			pipeReader.CloseWithError(err)
			return "", fmt.Errorf("failed to setup local backup: %w", err)
		}
		localPath = cfg.LocalPath
		uploadReader = io.TeeReader(pipeReader, localFile)
	}

	// Upload to R2
	_, err := uploader.Upload(ctx, cfg.Bucket, cfg.Key, uploadReader)
	if err != nil {
		// Cleanup partial local file on upload failure
		if localFile != nil {
			localFile.Close()
			os.Remove(cfg.LocalPath)
		}
		pipeReader.CloseWithError(err)
		return "", fmt.Errorf("R2 upload failed: %w", err)
	}

	// Finalize local backup
	if localFile != nil {
		if err := localFile.Close(); err != nil {
			os.Remove(cfg.LocalPath)
			return "", fmt.Errorf("failed to close local backup: %w", err)
		}
	}

	pipeReader.Close()
	return localPath, nil
}

// addDirToTar walks a directory and adds all files to the tar archive
func addDirToTar(ctx context.Context, tw *tar.Writer, root string) error {
	if ctx == nil {
		return ErrPushContextNil
	}
	if tw == nil {
		return errors.New("tar writer cannot be nil")
	}

	baseName := filepath.Base(root)

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return err
		}

		// Calculate relative path within the tar
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Prefix with directory name (e.g., "world/level.dat")
		tarPath := filepath.ToSlash(filepath.Join(baseName, relPath))
		if relPath == "." {
			tarPath = baseName + "/"
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = tarPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content (skip directories)
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		return nil
	})
}

// setupLocalBackup creates the local backup file
func setupLocalBackup(path string) (*os.File, error) {
	if path == "" {
		return nil, errors.New("path cannot be empty")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create local backup file: %w", err)
	}

	return file, nil
}

// countingWriter wraps a writer and counts bytes written
type countingWriter struct {
	w io.Writer
	n *int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c == nil || c.w == nil {
		return 0, errors.New("counting writer not initialized")
	}
	n, err := c.w.Write(p)
	if c.n != nil {
		*c.n += int64(n)
	}
	return n, err
}
