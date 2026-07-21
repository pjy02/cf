package lock

import (
	"fmt"
	"os"
	"time"
)

type Lock struct{ path string }

func Acquire(path string) (*Lock, error) {
	if err := os.Mkdir(path, 0o700); err == nil {
		return &Lock{path: path}, nil
	}
	info, err := os.Stat(path)
	if err == nil && time.Since(info.ModTime()) > 30*time.Minute {
		if removeErr := os.Remove(path); removeErr == nil {
			if mkdirErr := os.Mkdir(path, 0o700); mkdirErr == nil {
				return &Lock{path: path}, nil
			}
		}
	}
	return nil, fmt.Errorf("另一个同步任务正在运行（锁：%s）", path)
}

func (l *Lock) Release() { _ = os.Remove(l.path) }
