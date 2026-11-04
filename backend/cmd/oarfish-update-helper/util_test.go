package main

import (
	"encoding/json"
	"sort"
	"syscall"
	"time"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func sortStrings(s []string) []string { sort.Strings(s); return s }

func syscallMkfifo(path string) error { return syscall.Mkfifo(path, 0o644) }

func after2s() <-chan time.Time { return time.After(2 * time.Second) }
