//go:build !linux

// 非 Linux 平台的占位实现：保证 sbt 在 Windows/macOS 上能编译，
// 运行时给出明确提示（服务端只跑在 Linux 上）。
package sysinfo

import (
	"errors"
	"time"
)

var errNotLinux = errors.New("sbt 服务端仅支持 Linux")

type MemInfo struct{ TotalKB, AvailKB, SwapTotalKB, SwapFreeKB uint64 }
type Load struct{ One, Five, Fifteen float64 }
type OSInfo struct{ ID, Version, Pretty string }
type DiskInfo struct{ TotalBytes, FreeBytes uint64 }
type Proc struct{ PID int; Comm string; RSSKB uint64 }
type Port struct{ Proto, LocalAddr, State string; PID int; Comm string }

func ReadMemInfo() (MemInfo, error)          { return MemInfo{}, errNotLinux }
func ReadLoad() (Load, error)                { return Load{}, errNotLinux }
func Uptime() (time.Duration, error)         { return 0, errNotLinux }
func ReadOSInfo() (OSInfo, error)            { return OSInfo{}, errNotLinux }
func Kernel() (string, error)                { return "", errNotLinux }
func ReadDisk(string) (DiskInfo, error)      { return DiskInfo{}, errNotLinux }
func CPUUsage(time.Duration) (float64, error) { return 0, errNotLinux }
func NetRate(time.Duration) (float64, float64, error) { return 0, 0, errNotLinux }
func Procs() ([]Proc, error)                 { return nil, errNotLinux }
func Ports() ([]Port, error)                 { return nil, errNotLinux }
func WriteHeartbeat(string) error            { return errNotLinux }
