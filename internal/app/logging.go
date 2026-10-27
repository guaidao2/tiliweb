package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"tiliweb/internal/config"
)

type rotatingFile struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxBackups int
	f          *os.File
	size       int64
}

func newRotatingFile(path string, maxSizeMB, maxBackups int) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path, maxSize: int64(maxSizeMB) * 1024 * 1024, maxBackups: maxBackups}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	info, _ := f.Stat()
	r.f = f
	if info != nil {
		r.size = info.Size()
	}
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.maxSize > 0 && r.size+int64(len(p)) > r.maxSize && r.f != nil {
		_ = r.f.Close()
		max := r.maxBackups
		if max < 1 {
			max = 1
		}
		for i := max; i >= 1; i-- {
			src := r.path + "." + itoa(i)
			dst := r.path + "." + itoa(i+1)
			if i == max {
				_ = os.Remove(src)
				continue
			}
			_ = os.Rename(src, dst)
		}
		_ = os.Rename(r.path, r.path+".1")
		r.size = 0
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil {
		return r.f.Close()
	}
	return nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func newLogger(cfg *config.Config) (*slog.Logger, func() error, error) {
	level := slog.LevelInfo
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}

	writers := []io.Writer{os.Stdout}
	var closers []io.Closer
	if cfg.Log.File != "" {
		f, err := newRotatingFile(cfg.Log.File, cfg.Log.MaxSizeMB, cfg.Log.MaxBackups)
		if err != nil {
			return nil, nil, err
		}
		writers = append(writers, f)
		closers = append(closers, f)
	}

	w := io.MultiWriter(writers...)
	var handler slog.Handler
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	logger := slog.New(handler)
	closeFn := func() error {
		for _, c := range closers {
			_ = c.Close()
		}
		return nil
	}
	return logger, closeFn, nil
}
