package cachefs

import (
	"os"
	"testing"
	"time"
)

func BenchmarkCacheFS_LFU_Eviction(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithEvictionPolicy(EvictionLFU),
		WithMaxBytes(1000),
	)

	testData := make([]byte, 100)
	for i := range testData {
		testData[i] = byte('A' + (i % 26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 20
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = testData

		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_TTL_Eviction(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithEvictionPolicy(EvictionTTL),
		WithTTL(1*time.Millisecond),
		WithMaxBytes(1000),
	)

	testData := make([]byte, 100)
	for i := range testData {
		testData[i] = byte('A' + (i % 26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 20
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = testData

		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_Hybrid_Eviction(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithEvictionPolicy(EvictionHybrid),
		WithMaxBytes(1000),
	)

	testData := make([]byte, 100)
	for i := range testData {
		testData[i] = byte('A' + (i % 26))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 20
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = testData

		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, len(testData))
		file.Read(buf)
		file.Close()
	}
}

func BenchmarkCacheFS_MetadataCache_Hit(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithMetadataCache(true),
		WithMetadataMaxEntries(100),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Stat("/file.txt")
	}
}

func BenchmarkCacheFS_InvalidatePrefix(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Populate cache with files
	for i := 0; i < 10; i++ {
		filename := string([]byte{byte('/'), byte('d'), byte('a'), byte('t'), byte('a'), byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = []byte("test")
		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, 4)
		file.Read(buf)
		file.Close()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Repopulate between runs
		for j := 0; j < 10; j++ {
			filename := string([]byte{byte('/'), byte('d'), byte('a'), byte('t'), byte('a'), byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + j), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[filename] = []byte("test")
			file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
			buf := make([]byte, 4)
			file.Read(buf)
			file.Close()
		}
		b.StartTimer()

		cache.InvalidatePrefix("/data")
	}
}

func BenchmarkCacheFS_InvalidatePattern(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Populate cache with files
	for i := 0; i < 10; i++ {
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = []byte("test")
		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, 4)
		file.Read(buf)
		file.Close()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Repopulate between runs
		for j := 0; j < 10; j++ {
			filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + j), byte('.'), byte('t'), byte('x'), byte('t')})
			backing.files[filename] = []byte("test")
			file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
			buf := make([]byte, 4)
			file.Read(buf)
			file.Close()
		}
		b.StartTimer()

		cache.InvalidatePattern("/*.txt")
	}
}

func BenchmarkCacheFS_CleanExpired(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithTTL(1*time.Millisecond),
	)

	// Populate cache with files
	for i := 0; i < 10; i++ {
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[filename] = []byte("test")
		file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
		buf := make([]byte, 4)
		file.Read(buf)
		file.Close()
	}

	// Wait for expiration
	time.Sleep(2 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.CleanExpired()
	}
}

func BenchmarkCacheFS_TTL_With_Expiration(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithTTL(10*time.Millisecond),
	)

	backing.files["/file.txt"] = []byte("test data with TTL")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		file, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
		buf := make([]byte, 18)
		file.Read(buf)
		file.Close()

		// Occasionally wait for expiration
		if i%100 == 99 {
			time.Sleep(11 * time.Millisecond)
		}
	}
}

func BenchmarkCacheFS_MetadataCache_Miss(b *testing.B) {
	backing := newMockFS()
	cache := New(backing,
		WithMetadataCache(true),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % 100
		filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
		cache.Stat(filename)
	}
}

func BenchmarkCacheFS_EvictionPolicy_Comparison(b *testing.B) {
	policies := []struct {
		name   string
		policy EvictionPolicy
	}{
		{"LRU", EvictionLRU},
		{"LFU", EvictionLFU},
		{"TTL", EvictionTTL},
		{"Hybrid", EvictionHybrid},
	}

	testData := make([]byte, 100)
	for i := range testData {
		testData[i] = byte('A' + (i % 26))
	}

	for _, p := range policies {
		b.Run(p.name, func(b *testing.B) {
			backing := newMockFS()
			cache := New(backing,
				WithEvictionPolicy(p.policy),
				WithMaxBytes(1000),
			)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				idx := i % 20
				filename := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + idx/10), byte('0' + idx%10), byte('.'), byte('t'), byte('x'), byte('t')})
				backing.files[filename] = testData

				file, _ := cache.OpenFile(filename, os.O_RDONLY, 0644)
				buf := make([]byte, len(testData))
				file.Read(buf)
				file.Close()
			}
		})
	}
}
