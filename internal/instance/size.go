package instance

import (
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidSize is returned when a machine is asked for in a size that
// isn't offered.
var ErrInvalidSize = errors.New("invalid machine size")

// Size is how much machine an instance gets.
type Size struct {
	CPUs     int
	MemoryMB int
	// DiskGB is the size of the home folder's volume.
	DiskGB int
}

// Power is one of the levels of CPU and memory a machine can have.
type Power struct {
	// Name is what the API and the assistant call it.
	Name     string
	CPUs     int
	MemoryMB int
}

const (
	defaultPower  = "medium"
	defaultDiskGB = 10
)

// powers are the levels on offer, smallest first. They become per-plan once
// there are plans.
var powers = []Power{
	{Name: "small", CPUs: 1, MemoryMB: 1024},
	{Name: "medium", CPUs: 2, MemoryMB: 2048},
	{Name: "large", CPUs: 4, MemoryMB: 4096},
}

// disks are the disk sizes on offer, in GB.
var disks = []int{10, 20, 50}

// Powers returns the power levels a machine can be created with, smallest
// first.
func Powers() []Power {
	return slices.Clone(powers)
}

// Disks returns the disk sizes, in GB, a machine can be created with.
func Disks() []int {
	return slices.Clone(disks)
}

// SizeOf returns the size of a machine with the named power and the given
// disk. An empty name and a zero disk mean the defaults.
func SizeOf(power string, diskGB int) (Size, error) {
	if power == "" {
		power = defaultPower
	}
	if diskGB == 0 {
		diskGB = defaultDiskGB
	}

	i := slices.IndexFunc(powers, func(p Power) bool { return p.Name == power })
	if i < 0 {
		return Size{}, fmt.Errorf("%w: no power level called %q", ErrInvalidSize, power)
	}
	if !slices.Contains(disks, diskGB) {
		return Size{}, fmt.Errorf("%w: no %d GB disk", ErrInvalidSize, diskGB)
	}

	return Size{
		CPUs:     powers[i].CPUs,
		MemoryMB: powers[i].MemoryMB,
		DiskGB:   diskGB,
	}, nil
}

// offered reports whether the size is one a machine may have.
func (s Size) offered() bool {
	hasPower := slices.ContainsFunc(powers, func(p Power) bool {
		return p.CPUs == s.CPUs && p.MemoryMB == s.MemoryMB
	})

	return hasPower && slices.Contains(disks, s.DiskGB)
}
