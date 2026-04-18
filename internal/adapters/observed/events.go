// Package observed wraps a ports.StorageRepository and publishes one
// completion event per method call. Events live next to the publisher so the
// taxonomy stays close to the wiring.
package observed

import "fmt"

// StorageGetInfo is published by observedStorage.Get after the inner adapter
// returns. Bytes is len(data) on success, 0 on error. Err is nil on success.
type StorageGetInfo struct {
	Store      string
	Key        string
	Bytes      int
	DurationMs int64
	Err        error
}

func (s StorageGetInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.get store=%s key=%s err=%v dur=%dms", s.Store, s.Key, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.get store=%s key=%s bytes=%d dur=%dms", s.Store, s.Key, s.Bytes, s.DurationMs)
}

// StoragePutInfo is published by observedStorage.Put.
type StoragePutInfo struct {
	Store      string
	Key        string
	Bytes      int
	DurationMs int64
	Err        error
}

func (s StoragePutInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.put store=%s key=%s err=%v dur=%dms", s.Store, s.Key, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.put store=%s key=%s bytes=%d dur=%dms", s.Store, s.Key, s.Bytes, s.DurationMs)
}

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

// StorageRenameInfo is published by observedStorage.Rename.
type StorageRenameInfo struct {
	Store      string
	SrcKey     string
	DstKey     string
	DurationMs int64
	Err        error
}

func (s StorageRenameInfo) String() string {
	if s.Err != nil {
		return fmt.Sprintf("storage.rename store=%s %s→%s err=%v dur=%dms", s.Store, s.SrcKey, s.DstKey, s.Err, s.DurationMs)
	}
	return fmt.Sprintf("storage.rename store=%s %s→%s dur=%dms", s.Store, s.SrcKey, s.DstKey, s.DurationMs)
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
