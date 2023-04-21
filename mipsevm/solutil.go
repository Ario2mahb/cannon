package main

import (
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

type LineCol struct {
	Line uint32
	Col  uint32
}

type InstrMapping struct {
	S int32 // start offset in bytes within source (negative when non-existent!)
	L int32 // length in bytes within source (negative when non-existent!)
	F int32 // file index of source (negative when non-existent!)
	J byte  // jump type (i=into, o=out, -=regular)
	M int32 // modifier depth
}

func parseInstrMapping(last InstrMapping, v string) (InstrMapping, error) {
	data := strings.Split(v, ":")
	out := last
	if len(data) < 1 {
		return out, nil
	}
	if len(data) > 5 {
		return out, fmt.Errorf("unexpected length: %d", len(data))
	}
	var err error
	parse := func(x string) int32 {
		p, e := strconv.ParseInt(x, 10, 32)
		err = e
		return int32(p)
	}
	if data[0] != "" {
		out.S = parse(data[0])
	}
	if len(data) < 2 || err != nil {
		return out, err
	}
	if data[1] != "" {
		out.L = parse(data[1])
	}
	if len(data) < 3 || err != nil {
		return out, err
	}
	if data[2] != "" {
		out.F = parse(data[2])
	}
	if len(data) < 4 || err != nil {
		return out, err
	}
	if data[3] != "" {
		out.J = data[3][0]
	}
	if len(data) < 5 || err != nil {
		return out, err
	}
	if data[4] != "" {
		out.M = parse(data[4])
	}
	return out, err
}

type SourceMap struct {
	// source names
	Sources []string
	// per source, source offset -> line/col
	PosData [][]LineCol
	// per bytecode byte, byte index -> instr
	Instr []InstrMapping
}

func (s *SourceMap) Info(pc uint64) (source string, line uint32, col uint32) {
	instr := s.Instr[pc]
	if instr.F < 0 {
		return
	}
	if instr.F >= int32(len(s.Sources)) {
		source = "unknown"
		return
	}
	source = s.Sources[instr.F]
	if instr.S < 0 {
		return
	}
	if s.PosData[instr.F] == nil { // when the source file is known to be unavailable
		return
	}
	lc := s.PosData[instr.F][instr.S]
	line = lc.Line
	col = lc.Col
	return
}

func (s *SourceMap) FormattedInfo(pc uint64) string {
	f, l, c := s.Info(pc)
	return fmt.Sprintf("%s:%d:%d %v", f, l, c, s.Instr[pc])
}

// ParseSourceMap parses a solidity sourcemap: mapping bytecode indices to source references.
// See https://docs.soliditylang.org/en/latest/internals/source_mappings.html
func ParseSourceMap(sources []string, bytecode []byte, sourceMap string) (*SourceMap, error) {
	instructions := strings.Split(sourceMap, ";")

	srcMap := &SourceMap{
		Sources: sources,
		PosData: make([][]LineCol, 0, len(sources)),
		Instr:   make([]InstrMapping, 0, len(bytecode)),
	}
	// map source code position byte offsets to line/column pairs
	for i, s := range sources {
		if strings.HasPrefix(s, "~") {
			srcMap.PosData = append(srcMap.PosData, nil)
			continue
		}
		dat, err := os.ReadFile(s)
		if err != nil {
			return nil, fmt.Errorf("failed to read source %d %q: %w", i, s, err)
		}
		datStr := string(dat)

		out := make([]LineCol, len(datStr))
		line := uint32(1)
		lastLinePos := uint32(0)
		for i, b := range datStr { // iterate the utf8 or the bytes?
			col := uint32(i) - lastLinePos
			out[i] = LineCol{Line: line, Col: col}
			if b == '\n' {
				lastLinePos = uint32(i)
				line += 1
			}
		}
		srcMap.PosData = append(srcMap.PosData, out)
	}

	instIndex := 0

	// bytecode offset to instruction
	lastInstr := InstrMapping{}
	for i := 0; i < len(bytecode); {
		inst := bytecode[i]
		instLen := 1
		if inst >= 0x60 && inst <= 0x7f { // push instructions
			pushDataLen := inst - 0x60 + 1
			instLen += int(pushDataLen)
		}

		var instMapping string
		if instIndex >= len(instructions) {
			// truncated source-map? Or some instruction that's longer than we accounted for?
			// probably the contract-metadata bytes that are not accounted for in source map
			fmt.Printf("out of instructions: %d\n", instIndex-len(instructions))
		} else {
			instMapping = instructions[instIndex]
		}
		m, err := parseInstrMapping(lastInstr, instMapping)
		if err != nil {
			return nil, fmt.Errorf("failed to parse instr element in source map: %w", err)
		}

		for j := 0; j < instLen; j++ {
			srcMap.Instr = append(srcMap.Instr, m)
		}
		i += instLen
		instIndex += 1
	}
	return srcMap, nil
}

func (s *SourceMap) Tracer(out io.Writer) *SourceMapTracer {
	return &SourceMapTracer{s, out}
}

type SourceMapTracer struct {
	srcMap *SourceMap
	out    io.Writer
}

func (s *SourceMapTracer) CaptureTxStart(gasLimit uint64) {}

func (s *SourceMapTracer) CaptureTxEnd(restGas uint64) {}

func (s *SourceMapTracer) CaptureStart(env *vm.EVM, from common.Address, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
}

func (s *SourceMapTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {}

func (s *SourceMapTracer) CaptureEnter(typ vm.OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
}

func (s *SourceMapTracer) CaptureExit(output []byte, gasUsed uint64, err error) {}

func (s *SourceMapTracer) CaptureState(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, rData []byte, depth int, err error) {
	fmt.Fprintf(s.out, "%s: pc %x opcode %s  map %v\n", s.srcMap.FormattedInfo(pc), pc, op.String(), s.srcMap.Instr[pc])
}

func (s *SourceMapTracer) CaptureFault(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, depth int, err error) {
	fmt.Fprintf(s.out, "%s: FAULT %v\n", s.srcMap.FormattedInfo(pc), err)
}

var _ vm.EVMLogger = (*SourceMapTracer)(nil)