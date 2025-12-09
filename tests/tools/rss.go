package tools

import (
	"syscall"
	"unsafe"
)

var (
	modpsapi                 = syscall.NewLazyDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

type PROCESS_MEMORY_COUNTERS struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func GetProcessMemoryInfo(handle syscall.Handle, memCounters *PROCESS_MEMORY_COUNTERS, cb uint32) (err error) {
	r1, _, e1 := syscall.SyscallN(procGetProcessMemoryInfo.Addr(), uintptr(handle), uintptr(unsafe.Pointer(memCounters)), uintptr(cb))
	if r1 == 0 {
		err = e1
	}
	return
}

func GetRssMb() (uint64, error) {
	var m PROCESS_MEMORY_COUNTERS
	p, _ := syscall.GetCurrentProcess()
	err := GetProcessMemoryInfo(p, &m, uint32(unsafe.Sizeof(m)))
	if err != nil {
		return 0, err
	}
	return uint64(m.PeakWorkingSetSize) / 1024 / 1024, nil
}
