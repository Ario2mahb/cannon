package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	uc "github.com/unicorn-engine/unicorn/bindings/go/unicorn"
)

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

var steps int = 0
var heap_start uint64 = 0

func RegRead(u *uc.Unicorn, reg int) {

}

func RunUnicorn(fn string) {
	mu, err := uc.NewUnicorn(uc.ARCH_MIPS, uc.MODE_32|uc.MODE_BIG_ENDIAN)
	check(err)

	mu.HookAdd(uc.HOOK_INTR, func(mu uc.Unicorn, intno uint32) {
		if intno != 17 {
			log.Fatal("invalid interrupt ", intno, " at step ", steps)
		}
		syscall_no, _ := mu.RegRead(uc.MIPS_REG_V0)
		v0 := uint64(0)
		if syscall_no == 4004 {
			buf, _ := mu.RegRead(uc.MIPS_REG_A1)
			count, _ := mu.RegRead(uc.MIPS_REG_A2)
			bytes, _ := mu.MemRead(buf, count)
			os.Stderr.Write(bytes)
		} else if syscall_no == 4090 {
			a0, _ := mu.RegRead(uc.MIPS_REG_A0)
			sz, _ := mu.RegRead(uc.MIPS_REG_A1)
			if a0 == 0 {
				v0 = 0x20000000 + heap_start
				heap_start += sz
			} else {
				v0 = a0
			}
		} else if syscall_no == 4045 {
			v0 = 0x40000000
		} else if syscall_no == 4120 {
			v0 = 1
		} else {
			fmt.Println("syscall", syscall_no)
		}
		mu.RegWrite(uc.MIPS_REG_V0, v0)
		mu.RegWrite(uc.MIPS_REG_A3, 0)
	}, 1, 0)

	ministart := time.Now()
	mu.HookAdd(uc.HOOK_CODE, func(mu uc.Unicorn, addr uint64, size uint32) {
		if steps%100000 == 0 {
			steps_per_sec := float64(steps) * 1e9 / float64(time.Now().Sub(ministart).Nanoseconds())
			fmt.Printf("%6d Code: 0x%x, 0x%x steps per s %f\n", steps, addr, size, steps_per_sec)
		}
		steps += 1
	}, 1, 0)

	check(mu.MemMap(0, 0x80000000))

	dat, _ := ioutil.ReadFile(fn)
	mu.MemWrite(0, dat)

	mu.Start(0, 0xdead0000)

}