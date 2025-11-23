package cachefs

import (
	"os"
	"testing"
)

func BenchmarkCacheFS_ReadHit(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Populate cache
	testData := []byte("benchmark test data")
	backing.files["/bench.txt"] = testData

	// First read to populate cache
	file, _ := cache.OpenFile("/bench.txt", os.O_RDONLY, 0644)
	buf := make([]byte, len(testData))
	file.Read(buf)
	file.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/bench.txt", os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_ReadMiss(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("benchmark test data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different filenames to avoid cache hits
		filename := string([]byte{byte('/'), byte('b'), byte('e'), byte('n'), byte('c'), byte('h'), byte('0' + (i % 10)), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = testData

		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()

		// Clean up to prevent memory growth
		delete(backing.files, filename)
		cache.Invalidate(filename)
	}
}

func BenchmarkCacheFS_WriteThrough(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteThrough))

	testData := []byte("benchmark write data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

func BenchmarkCacheFS_WriteBack(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack))

	testData := []byte("benchmark write data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/bench.txt", os.O_WRONLY|os.O_CREATE, 0644)
		file.Write(testData)
		file.Close()
	}
}

func BenchmarkCacheFS_LRU_Eviction(b *testing.B) {
	backing := newMockFS()
	cache := New(backing, WithMaxBytes(1000)) // Small cache to force evictions

	testData := make([]byte, 100)
	for i := range testData {
		testData[i] = byte('A' + (i % 26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create unique filename
		idx := i % 20
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = testData

		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_Sequential_Reads(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	testData := make([]byte, 1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	backing.files["/large.txt"] = testData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/large.txt", os.O_RDONLY, 0644)
		buf := make([]byte, 1024)
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_Random_Access(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Create 10 files
	for i := 0; i < 10; i++ {
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = []byte("test data for file")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 10
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx), byte('.'), byte('t'), byte('x'), byte('t')})
		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, 18)
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_Stats_Tracking(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("stats benchmark")
	backing.files["/stats.txt"] = testData

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/stats.txt", os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()

		// Access stats
		_ = cache.Stats().HitRate()
		_ = cache.Stats().Hits()
		_ = cache.Stats().Misses()
	}
}

func BenchmarkCacheFS_Invalidate(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("invalidate benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		backing.files["/inv.txt"] = testData
		file, _ := cache.OpenFile("/inv.txt", os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()
		b.StartTimer()

		cache.Invalidate("/inv.txt")
	}
}

func BenchmarkCacheFS_Clear(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("clear benchmark")

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Populate cache with 10 files
		for j := 0; j < 10; j++ {
			filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + j), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[filename] = testData
			file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
			buf := make([]byte, len(testData))
			file.Read(buf)
			file.Close()
		}
		b.StartTimer()

		cache.Clear()
	}
}
