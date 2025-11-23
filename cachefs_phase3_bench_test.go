package cachefs

import (
	"os"
	"testing"
	"time"
)

// BenchmarkWriteBack_SingleFile benchmarks write-back mode performance
func BenchmarkWriteBack_SingleFile(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	testData := []byte("write-back benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

// BenchmarkWriteBack_MultipleFiles benchmarks write-back with many files
func BenchmarkWriteBack_MultipleFiles(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	testData := []byte("test data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 10
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx), byte('.'), byte('t'), byte('x'), byte('t')})
		file, _ := cache.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

// BenchmarkWriteBack_Flush benchmarks manual flush operations
func BenchmarkWriteBack_Flush(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	// Populate with dirty entries
	testData := []byte("dirty data")
	for i := 0; i < 10; i++ {
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		file, _ := cache.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Flush()
	}
}

// BenchmarkWriteBack_FlushOnClose benchmarks flush on file close
func BenchmarkWriteBack_FlushOnClose(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithFlushOnClose(true),
		WithFlushInterval(1*time.Hour),
	)
	defer cache.Close()

	testData := []byte("flush on close data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close() // Will trigger flush
	}
}

// BenchmarkWriteBack_LargeWrites benchmarks large write operations
func BenchmarkWriteBack_LargeWrites(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	testData := make([]byte, 1024*1024) // 1 MB
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/large.bin", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

// BenchmarkWriteMode_Comparison compares all write modes
func BenchmarkWriteMode_Comparison(b *testing.B) {
	modes := []struct {
		name string
		mode WriteMode
	}{
		{"WriteThrough", WriteModeWriteThrough},
		{"WriteBack", WriteModeWriteBack},
		{"WriteAround", WriteModeWriteAround},
	}

	testData := []byte("comparison benchmark data")

	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			backing := newMockFS()
			cache := New(backing, WithWriteMode(m.mode), WithFlushInterval(1*time.Hour))
			defer cache.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
				file.Write(testData)
				file.Close()
			}
		})
	}
}

// BenchmarkWriteBack_DirtyEviction benchmarks evicting dirty entries
func BenchmarkWriteBack_DirtyEviction(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithMaxBytes(1000), // Small cache to force evictions
		WithFlushInterval(1*time.Hour),
	)
	defer cache.Close()

	testData := make([]byte, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 20
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
		file, _ := cache.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

// BenchmarkWriteBack_ConcurrentWrites benchmarks concurrent write operations
func BenchmarkWriteBack_ConcurrentWrites(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	testData := []byte("concurrent write data")

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % 10
			filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := cache.OpenFile(filename, os.O_WRONLY|os.O_CREATE, 0644)
			file.Write(testData)
			file.Close()
			i++
		}
	})
}

// BenchmarkWriteBack_BackgroundFlush benchmarks background flush performance
func BenchmarkWriteBack_BackgroundFlush(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithFlushInterval(10*time.Millisecond), // Frequent flushes
	)
	defer cache.Close()

	testData := []byte("background flush data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

// BenchmarkWriteBack_AccumulatedWrites benchmarks multiple writes to same file
func BenchmarkWriteBack_AccumulatedWrites(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	testData := []byte("accumulated data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/accumulated.txt", os.O_WRONLY|os.O_CREATE, 0644)
		for j := 0; j < 10; j++ {
			file.Write(testData)
		}
		file.Close()
	}
}

// BenchmarkWriteBack_ReadAfterWrite benchmarks read after write in write-back mode
func BenchmarkWriteBack_ReadAfterWrite(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1*time.Hour))
	defer cache.Close()

	testData := []byte("read after write data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Write
		file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()

		// Read (should read from cache)
		file2, _ := cache.OpenFile("/bench.txt", os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file2.Read(buf)
		file2.Close()
	}
}
