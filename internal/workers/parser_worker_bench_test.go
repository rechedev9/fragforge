package workers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/storage"
)

func BenchmarkOpenDemoLocal(b *testing.B) {
	const size = int64(64 << 20)

	store, err := storage.NewLocal(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	const key = "demos/benchmark.dem"
	path, err := store.ResolvePath(key)
	if err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		b.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}

	worker := NewParserWorker(nil, store)
	b.ReportMetric(float64(size)/(1<<20), "source-MiB")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		demo, cleanup, err := worker.openDemo(key)
		if err != nil {
			b.Fatal(err)
		}
		info, err := demo.Stat()
		if err != nil {
			cleanup()
			b.Fatal(err)
		}
		if info.Size() != size {
			cleanup()
			b.Fatalf("demo size = %d, want %d", info.Size(), size)
		}
		cleanup()
	}
}
