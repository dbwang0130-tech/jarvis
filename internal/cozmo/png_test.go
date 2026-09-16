package cozmo

import (
	"os"
	"testing"
)

func TestRenderPNG(t *testing.T) {
	for _, name := range []string{"Neutral", "Happiness", "Anger", "Asleep"} {
		e := Expressions[name]
		f := NewFace(e.Params, 256, 128)
		data, err := f.PNG(256, 128, 4)
		if err != nil {
			t.Fatal(err)
		}
		path := "/tmp/cozmo-" + name + ".png"
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("已生成 %s", path)
	}
}
