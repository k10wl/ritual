// Package observed wraps a ports.StorageRepository and publishes one
// completion event per method call. Events live next to the publisher so the
// taxonomy stays close to the wiring.
package observed

import "fmt"

// StorageCopyInfo is published by observedStorage.Copy.
type StorageCopyInfo struct {
	Store      string
	SrcKey     string
	DstKey     string
	DurationMs int64
	Err        error
}

func (s StorageCopyInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.copy store=%s %s→%s err=%v dur=%dms", s.Store, s.SrcKey, s.DstKey, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.copy store=%s %s→%s dur=%dms", s.Store, s.SrcKey, s.DstKey, s.DurationMs)
}

// StorageDeleteInfo is published by observedStorage.Delete (single tree-delete).
type StorageDeleteInfo struct {
	Store      string
	Key        string
	DurationMs int64
	Err        error
}

func (s StorageDeleteInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.delete store=%s key=%s err=%v dur=%dms", s.Store, s.Key, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.delete store=%s key=%s dur=%dms", s.Store, s.Key, s.DurationMs)
}

// StorageDeleteBatchInfo is published by observedStorage.DeleteBatch.
type StorageDeleteBatchInfo struct {
	Store      string
	Keys       []string
	DurationMs int64
	Err        error
}

func (s StorageDeleteBatchInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.deletebatch store=%s count=%d err=%v dur=%dms", s.Store, len(s.Keys), s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.deletebatch store=%s count=%d dur=%dms", s.Store, len(s.Keys), s.DurationMs)
}

// StorageListInfo is published by observedStorage.List.
type StorageListInfo struct {
	Store      string
	Prefix     string
	Count      int
	DurationMs int64
	Err        error
}

func (s StorageListInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.list store=%s prefix=%s err=%v dur=%dms", s.Store, s.Prefix, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.list store=%s prefix=%s count=%d dur=%dms", s.Store, s.Prefix, s.Count, s.DurationMs)
}

// StorageGetStreamInfo is published by observedStorage.GetStream after the
// streamed body is closed (or right away on open-error). Bytes is the cumulative
// count read from the body; 0 on open-error.
type StorageGetStreamInfo struct {
	Store      string
	Key        string
	Bytes      int64
	DurationMs int64
	Err        error
}

func (s StorageGetStreamInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.getstream store=%s key=%s err=%v dur=%dms", s.Store, s.Key, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.getstream store=%s key=%s bytes=%d dur=%dms", s.Store, s.Key, s.Bytes, s.DurationMs)
}

// StoragePutStreamInfo is published by observedStorage.PutStream. Bytes is the
// body size discovered via Seek before the upload; value is recorded even on
// error so failed uploads still carry an intended size.
type StoragePutStreamInfo struct {
	Store      string
	Key        string
	Bytes      int64
	DurationMs int64
	Err        error
}

func (s StoragePutStreamInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.putstream store=%s key=%s err=%v dur=%dms", s.Store, s.Key, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.putstream store=%s key=%s bytes=%d dur=%dms", s.Store, s.Key, s.Bytes, s.DurationMs)
}

// StorageExistsInfo is published by observedStorage.Exists. Hit mirrors the
// bool returned to the caller; Err is non-nil on surface errors.
type StorageExistsInfo struct {
	Store      string
	Key        string
	Hit        bool
	DurationMs int64
	Err        error
}

func (s StorageExistsInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.exists store=%s key=%s err=%v dur=%dms", s.Store, s.Key, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.exists store=%s key=%s hit=%t dur=%dms", s.Store, s.Key, s.Hit, s.DurationMs)
}
