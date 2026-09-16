package cozmo

import (
	"strings"
	"testing"
)

func TestRenderSamples(t *testing.T) {
	// 象限块：原生 128×64 → 64 列（完整分辨率）
	for _, name := range []string{"Neutral", "Happiness", "Anger"} {
		e := Expressions[name]
		f := NewFace(e.Params, 128, 64)
		t.Logf("\n=== 象限块 %s（%s）128x64→64列 ===\n%s", name, e.ZH, strings.Join(ToQuadrantBlocks(f.Render()), "\n"))
	}

	// 象限块：64×32 → 32 列（小而清晰）
	for _, name := range []string{"Neutral", "Happiness", "Anger", "Asleep"} {
		e := Expressions[name]
		f := NewFace(e.Params, 64, 32)
		t.Logf("\n=== 象限块 %s（%s）64x32→32列 ===\n%s", name, e.ZH, strings.Join(ToQuadrantBlocks(f.Render()), "\n"))
	}
}
