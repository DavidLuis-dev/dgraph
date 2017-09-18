package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger"
	bo "github.com/dgraph-io/badger/options"
	"github.com/dgraph-io/dgraph/protos"
	"github.com/dgraph-io/dgraph/schema"
	"github.com/dgraph-io/dgraph/x"
)

type options struct {
	rdfDir          string
	schemaFile      string
	badgerDir       string
	leaseFile       string
	tmpDir          string
	numGoroutines   int
	mapBufSize      int64
	skipExpandEdges bool
}

type state struct {
	opt        options
	prog       *progress
	um         *uidMap
	ss         *schemaStore
	rdfChunkCh chan *bytes.Buffer
	mapFileId  uint32 // Used atomically to name the output files of the mappers.
	kv         *badger.KV
}

type loader struct {
	*state
	mappers []*mapper
}

func newLoader(opt options) *loader {
	schemaBuf, err := ioutil.ReadFile(opt.schemaFile)
	x.Checkf(err, "Could not load schema.")
	initialSchema, err := schema.Parse(string(schemaBuf))
	x.Checkf(err, "Could not parse schema.")

	st := &state{
		opt:  opt,
		prog: newProgress(),
		um:   newUIDMap(),
		ss:   newSchemaStore(initialSchema),

		// Lots of gz readers, so not much channel buffer needed.
		rdfChunkCh: make(chan *bytes.Buffer, opt.numGoroutines),
	}
	ld := &loader{
		state:   st,
		mappers: make([]*mapper, opt.numGoroutines),
	}
	for i := 0; i < opt.numGoroutines; i++ {
		ld.mappers[i] = &mapper{state: st}
	}
	go ld.prog.report()
	return ld
}

func readChunk(r *bufio.Reader) (*bytes.Buffer, error) {
	batch := new(bytes.Buffer)
	batch.Grow(10 << 20)
	for lineCount := 0; lineCount < 1e5; lineCount++ {
		slc, err := r.ReadSlice('\n')
		if err == io.EOF {
			batch.Write(slc)
			return batch, err
		}
		if err == bufio.ErrBufferFull {
			// This should only happen infrequently.
			batch.Write(slc)
			var str string
			str, err = r.ReadString('\n')
			if err == io.EOF {
				batch.WriteString(str)
				return batch, err
			}
			if err != nil {
				return nil, err
			}
			batch.WriteString(str)
		}
		if err != nil {
			return nil, err
		}
		batch.Write(slc)
	}
	return batch, nil
}

func findRDFFiles(dir string) []string {
	var files []string
	x.Check(filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".rdf") || strings.HasSuffix(path, ".rdf.gz") {
			files = append(files, path)
		}
		return nil
	}))
	return files
}

func (ld *loader) mapStage() {
	ld.prog.setPhase(mapPhase)

	var mapperWg sync.WaitGroup
	mapperWg.Add(len(ld.mappers))
	for _, m := range ld.mappers {
		go func(m *mapper) {
			m.run()
			mapperWg.Done()
		}(m)
	}

	var readers []*bufio.Reader
	for _, rdfFile := range findRDFFiles(ld.opt.rdfDir) {
		f, err := os.Open(rdfFile)
		x.Check(err)
		defer f.Close()
		if !strings.HasSuffix(rdfFile, ".gz") {
			readers = append(readers, bufio.NewReaderSize(f, 1<<20))
		} else {
			gzr, err := gzip.NewReader(f)
			x.Checkf(err, "Could not create gzip reader for RDF file %q.", rdfFile)
			readers = append(readers, bufio.NewReader(gzr))
		}
	}

	pending := make(chan struct{}, ld.opt.numGoroutines)
	for _, r := range readers {
		pending <- struct{}{}
		go func(r *bufio.Reader) {
			for {
				chunkBuf, err := readChunk(r)
				if err == io.EOF {
					if chunkBuf.Len() != 0 {
						ld.rdfChunkCh <- chunkBuf
					}
					break
				}
				x.Check(err)
				ld.rdfChunkCh <- chunkBuf
			}
			<-pending
		}(r)
	}
	for i := 0; i < ld.opt.numGoroutines; i++ {
		pending <- struct{}{}
	}

	close(ld.rdfChunkCh)
	mapperWg.Wait()

	// Allow memory to GC before the reduce phase.
	for i := range ld.mappers {
		ld.mappers[i] = nil
	}
	ld.writeLease()
	ld.um = nil
	runtime.GC()
}

func (ld *loader) writeLease() {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d\n", ld.um.lease)
	x.Check(ioutil.WriteFile(ld.opt.leaseFile, buf.Bytes(), 0644))
}

func (ld *loader) reduceStage() {
	ld.prog.setPhase(reducePhase)

	// Read output from map stage.
	var mapOutput []string
	err := filepath.Walk(ld.opt.tmpDir, func(path string, _ os.FileInfo, err error) error {
		if !strings.HasSuffix(path, ".map") {
			return nil
		}
		mapOutput = append(mapOutput, path)
		return nil
	})
	x.Checkf(err, "While walking the map output.")

	shuffleInputChs := make([]chan *protos.MapEntry, len(mapOutput))
	for i, mappedFile := range mapOutput {
		shuffleInputChs[i] = make(chan *protos.MapEntry, 1000)
		go readMapOutput(mappedFile, shuffleInputChs[i])
	}

	opt := badger.DefaultOptions
	opt.Dir = ld.opt.badgerDir
	opt.ValueDir = opt.Dir
	opt.ValueGCRunInterval = time.Hour * 100
	opt.SyncWrites = false
	opt.TableLoadingMode = bo.MemoryMap
	ld.kv, err = badger.NewKV(&opt)
	x.Check(err)

	// Shuffle concurrently with reduce.
	ci := &countIndexer{state: ld.state}
	// Small buffer size since each element has a lot of data.
	reduceCh := make(chan []*protos.MapEntry, 3)
	go shufflePostings(reduceCh, shuffleInputChs, ld.prog, ci)

	// Reduce stage.
	pending := make(chan struct{}, ld.opt.numGoroutines)
	for batch := range reduceCh {
		pending <- struct{}{}
		go func(batch []*protos.MapEntry) {
			reduce(batch, ld.kv, ld.prog)
			<-pending
		}(batch)
	}
	for i := 0; i < ld.opt.numGoroutines; i++ {
		pending <- struct{}{}
	}
	ci.wait()
}

func (ld *loader) writeSchema() {
	ld.ss.write(ld.kv)
}

func (ld *loader) cleanup() {
	ld.prog.endSummary()
	x.Check(ld.kv.Close())
}
