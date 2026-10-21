package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

const maxApplicationLogSize = 5 << 20

type logFanout []io.Writer

func (w logFanout) Write(payload []byte) (int, error) {
	var firstErr error
	for _, output := range w {
		written, err := output.Write(payload)
		if err != nil && firstErr == nil {
			firstErr = err
		} else if written != len(payload) && firstErr == nil {
			firstErr = io.ErrShortWrite
		}
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return len(payload), nil
}

type applicationLogFile struct {
	mu                sync.Mutex
	path              string
	maxBytes          int64
	file              *os.File
	closed            bool
	rotateOnNextWrite bool
}

func openApplicationLog(path string, maxBytes int64) (*applicationLogFile, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("application log size limit must be positive")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open application log file: %w", err)
	}
	return &applicationLogFile{path: path, maxBytes: maxBytes, file: file}, nil
}

func (w *applicationLogFile) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, os.ErrClosed
	}
	if w.file == nil {
		file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, fmt.Errorf("reopen application log file: %w", err)
		}
		w.file = file
	}
	if w.rotateOnNextWrite {
		info, err := w.file.Stat()
		if err != nil {
			w.discardFile()
			return 0, fmt.Errorf("stat damaged application log file: %w", err)
		}
		if info.Size() > 0 {
			if err := w.rotate(); err != nil {
				return 0, fmt.Errorf("rotate damaged application log: %w", err)
			}
		}
		w.rotateOnNextWrite = false
	}
	info, err := w.file.Stat()
	if err != nil {
		w.discardFile()
		return 0, fmt.Errorf("stat application log file: %w", err)
	}
	if info.Size() > 0 && info.Size()+int64(len(payload)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
		info, err = w.file.Stat()
		if err != nil {
			w.discardFile()
			return 0, fmt.Errorf("stat application log after rotation: %w", err)
		}
	}
	writeOffset := info.Size()
	written, err := w.file.Write(payload)
	if err != nil || written != len(payload) {
		rollbackErr := w.file.Truncate(writeOffset)
		w.discardFile()
		if err == nil {
			err = io.ErrShortWrite
		}
		if rollbackErr != nil {
			w.rotateOnNextWrite = written > 0
			return written, fmt.Errorf("write application log file: %w; roll back partial log record: %v", err, rollbackErr)
		}
		return written, fmt.Errorf("write application log file: %w", err)
	}
	return written, nil
}

func (w *applicationLogFile) discardFile() {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}

func (w *applicationLogFile) rotate() error {
	if err := w.file.Close(); err != nil {
		w.file = nil
		return fmt.Errorf("close application log before rotation: %w", err)
	}
	w.file = nil
	backupPath := w.path + ".1"
	// On the supported Windows platform, os.Rename replaces an existing file.
	// Keep the prior backup until the active log has been moved successfully.
	if err := os.Rename(w.path, backupPath); err != nil {
		return w.reopenAfterRotationFailure(fmt.Errorf("rotate application log: %w", err))
	}
	w.rotateOnNextWrite = false
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("reopen application log after rotation: %w", err)
	}
	w.file = file
	return nil
}

func (w *applicationLogFile) reopenAfterRotationFailure(cause error) error {
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w; reopen application log: %v", cause, err)
	}
	w.file = file
	return cause
}

func (w *applicationLogFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
