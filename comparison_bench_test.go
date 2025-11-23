package cachefs

import (
	"os"
	"testing"

	"github.com/absfs/corfs"
)

// BenchmarkComparison_ReadHit compares cache hit performance
func BenchmarkComparison_ReadHit(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithMaxBytes(10*1024*1024))

		// Pre-populate
		backing.files["/testfile.txt"] = make([]byte, 1024)
		file, _ := cache.OpenFile("/testfile.txt", os.O_RDONLY, 0644)
		buf := make([]byte, 1024)
		file.Read(buf)
		file.Close()

		// Reset stats for clean benchmark
		cache.ResetStats()

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			file, _ := cache.OpenFile("/testfile.txt", os.O_RDONLY, 0644)
			file.Read(buf)
			file.Close()
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Pre-populate
		primary.files["/testfile.txt"] = make([]byte, 1024)
		file, _ := fs.OpenFile("/testfile.txt", os.O_RDONLY, 0644)
		buf := make([]byte, 1024)
		file.Read(buf)
		file.Close()

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			file, _ := fs.OpenFile("/testfile.txt", os.O_RDONLY, 0644)
			file.Read(buf)
			file.Close()
		}
	})
}

// BenchmarkComparison_ReadMiss compares cache miss performance
func BenchmarkComparison_ReadMiss(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithMaxBytes(10*1024*1024))

		// Create files
		for i := 0; i < 1000; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/100)%10), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[path] = make([]byte, 1024)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			idx := i % 1000
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/100)%10), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 1024)
			file.Read(buf)
			file.Close()
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Create files
		for i := 0; i < 1000; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/100)%10), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			primary.files[path] = make([]byte, 1024)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			idx := i % 1000
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/100)%10), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := fs.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 1024)
			file.Read(buf)
			file.Close()
		}
	})
}

// BenchmarkComparison_WriteThrough compares write-through performance
func BenchmarkComparison_WriteThrough(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithWriteMode(WriteModeWriteThrough))

		data := make([]byte, 1024)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			file, _ := cache.OpenFile("/testfile.txt", os.O_WRONLY|os.O_CREATE, 0644)
			file.Write(data)
			file.Close()
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		data := make([]byte, 1024)

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			file, _ := fs.OpenFile("/testfile.txt", os.O_WRONLY|os.O_CREATE, 0644)
			file.Write(data)
			file.Close()
		}
	})
}

// BenchmarkComparison_SequentialReads compares sequential read performance
func BenchmarkComparison_SequentialReads(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithMaxBytes(50*1024*1024))

		// Create 100 files
		for i := 0; i < 100; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[path] = make([]byte, 4096)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (j/10)%10), byte('0' + j%10), byte('.'), byte('t'), byte('x'), byte('t')})
				file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
				buf := make([]byte, 4096)
				file.Read(buf)
				file.Close()
			}
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Create 100 files
		for i := 0; i < 100; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			primary.files[path] = make([]byte, 4096)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (j/10)%10), byte('0' + j%10), byte('.'), byte('t'), byte('x'), byte('t')})
				file, _ := fs.OpenFile(path, os.O_RDONLY, 0644)
				buf := make([]byte, 4096)
				file.Read(buf)
				file.Close()
			}
		}
	})
}

// BenchmarkComparison_RandomAccess compares random access performance
func BenchmarkComparison_RandomAccess(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithMaxBytes(10*1024*1024), WithEvictionPolicy(EvictionLRU))

		// Create 100 files
		for i := 0; i < 100; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[path] = make([]byte, 2048)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Random access pattern
			idx := (i * 17) % 100
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 2048)
			file.Read(buf)
			file.Close()
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Create 100 files
		for i := 0; i < 100; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			primary.files[path] = make([]byte, 2048)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			// Random access pattern
			idx := (i * 17) % 100
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := fs.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 2048)
			file.Read(buf)
			file.Close()
		}
	})
}

// BenchmarkComparison_EvictionLRU compares eviction performance
func BenchmarkComparison_EvictionLRU(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		// Small cache to trigger evictions
		cache := New(backing, WithMaxBytes(100*1024), WithEvictionPolicy(EvictionLRU))

		// Create files that will exceed cache
		for i := 0; i < 200; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/100)%10), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[path] = make([]byte, 1024)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			idx := i % 200
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/100)%10), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 1024)
			file.Read(buf)
			file.Close()
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Create files
		for i := 0; i < 200; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/100)%10), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			primary.files[path] = make([]byte, 1024)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			idx := i % 200
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/100)%10), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := fs.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 1024)
			file.Read(buf)
			file.Close()
		}
	})
}

// BenchmarkComparison_ConcurrentReads compares concurrent read performance
func BenchmarkComparison_ConcurrentReads(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithMaxBytes(10*1024*1024))

		// Pre-populate with 50 files
		for i := 0; i < 50; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[path] = make([]byte, 4096)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				idx := i % 50
				path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
				file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
				buf := make([]byte, 4096)
				file.Read(buf)
				file.Close()
				i++
			}
		})
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Pre-populate with 50 files
		for i := 0; i < 50; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			primary.files[path] = make([]byte, 4096)
		}

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				idx := i % 50
				path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
				file, _ := fs.OpenFile(path, os.O_RDONLY, 0644)
				buf := make([]byte, 4096)
				file.Read(buf)
				file.Close()
				i++
			}
		})
	})
}

// BenchmarkComparison_SmallFiles compares performance with many small files
func BenchmarkComparison_SmallFiles(b *testing.B) {
	b.Run("cachefs", func(b *testing.B) {
		backing := newMockFS()
		cache := New(backing, WithMaxBytes(5*1024*1024))

		// Create 500 small files
		for i := 0; i < 500; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/100)%10), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[path] = make([]byte, 256)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			idx := i % 500
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/100)%10), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 256)
			file.Read(buf)
			file.Close()
		}
	})

	b.Run("corfs", func(b *testing.B) {
		primary := newMockFS()
		cacheFS := newMockFS()
		fs := corfs.New(primary, cacheFS)

		// Create 500 small files
		for i := 0; i < 500; i++ {
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/100)%10), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
			primary.files[path] = make([]byte, 256)
		}

		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			idx := i % 500
			path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (idx/100)%10), byte('0' + (idx/10)%10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
			file, _ := fs.OpenFile(path, os.O_RDONLY, 0644)
			buf := make([]byte, 256)
			file.Read(buf)
			file.Close()
		}
	})
}
