package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validMediaStorageKey(key string) bool {
	return key != "" && key != "." && key != ".." && filepath.Base(key) == key && !strings.ContainsAny(key, `/\:`)
}

func mediaDeleteStagingPath(mediaRoot, mediaID string) string {
	digest := sha256.Sum256([]byte(mediaID))
	return filepath.Join(mediaRoot, ".delete-"+hex.EncodeToString(digest[:]))
}

func stageMediaFile(mediaRoot, mediaID, storageKey string) (originalPath, stagedPath string, staged bool, err error) {
	if !validMediaStorageKey(storageKey) {
		return "", "", false, errors.New("media storage key is invalid")
	}
	originalPath = filepath.Join(mediaRoot, storageKey)
	stagedPath = mediaDeleteStagingPath(mediaRoot, mediaID)

	stagedInfo, err := os.Lstat(stagedPath)
	if err == nil {
		if !stagedInfo.Mode().IsRegular() && stagedInfo.Mode()&os.ModeSymlink == 0 {
			return "", "", false, errors.New("staged media path is not a file")
		}
		if _, originalErr := os.Lstat(originalPath); originalErr == nil {
			return "", "", false, errors.New("both original and staged media files exist")
		} else if !errors.Is(originalErr, os.ErrNotExist) {
			return "", "", false, fmt.Errorf("inspect original media file: %w", originalErr)
		}
		return originalPath, stagedPath, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", "", false, fmt.Errorf("inspect staged media file: %w", err)
	}

	originalInfo, err := os.Lstat(originalPath)
	if errors.Is(err, os.ErrNotExist) {
		return originalPath, stagedPath, false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("inspect original media file: %w", err)
	}
	if !originalInfo.Mode().IsRegular() && originalInfo.Mode()&os.ModeSymlink == 0 {
		return "", "", false, errors.New("media path is not a file")
	}
	if err := os.Rename(originalPath, stagedPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return originalPath, stagedPath, false, nil
		}
		return "", "", false, fmt.Errorf("stage media file: %w", err)
	}
	return originalPath, stagedPath, true, nil
}

func restoreStagedMedia(originalPath, stagedPath string) error {
	stagedInfo, err := os.Lstat(stagedPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect staged media file: %w", err)
	}
	if !stagedInfo.Mode().IsRegular() && stagedInfo.Mode()&os.ModeSymlink == 0 {
		return errors.New("staged media path is not a file")
	}
	if _, err := os.Lstat(originalPath); err == nil {
		return errors.New("original media path is already occupied")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect original media path: %w", err)
	}
	if err := os.Rename(stagedPath, originalPath); err != nil {
		return fmt.Errorf("restore staged media file: %w", err)
	}
	return nil
}

func restoreStagedMediaIfMissing(mediaRoot, mediaID, storageKey string) error {
	if !validMediaStorageKey(storageKey) {
		return errors.New("media storage key is invalid")
	}
	originalPath := filepath.Join(mediaRoot, storageKey)
	if _, err := os.Lstat(originalPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect original media file: %w", err)
	}
	return restoreStagedMedia(originalPath, mediaDeleteStagingPath(mediaRoot, mediaID))
}

func removeStagedMedia(stagedPath string) error {
	if err := os.Remove(stagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
