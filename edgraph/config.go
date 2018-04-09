/*
 * Copyright 2017-2018 Dgraph Labs, Inc. and Contributors
 *
 * This file is available under the Apache License, Version 2.0,
 * with the Commons Clause restriction.
 */

package edgraph

import (
	"expvar"
	"fmt"
	"path/filepath"

	"github.com/dgraph-io/dgraph/posting"
	"github.com/dgraph-io/dgraph/worker"
	"github.com/dgraph-io/dgraph/x"
)

type Options struct {
	PostingDir    string
	PostingTables string
	WALDir        string
	Nomutations   bool

	AllottedMemory float64

	ExportPath          string
	NumPendingProposals int
	Tracing             float64
	MyAddr              string
	ZeroAddr            string
	RaftId              uint64
	MaxPendingCount     uint64
	ExpandEdge          bool

	DebugMode bool
}

// TODO(tzdybal) - remove global
var Config Options

var DefaultConfig = Options{
	PostingDir:    "p",
	PostingTables: "memorymap",
	WALDir:        "w",
	Nomutations:   false,

	// User must specify this.
	AllottedMemory: -1.0,

	ExportPath:          "export",
	NumPendingProposals: 2000,
	Tracing:             0.0,
	MyAddr:              "",
	ZeroAddr:            fmt.Sprintf("localhost:%d", x.PortZeroGrpc),
	MaxPendingCount:     100,
	ExpandEdge:          true,

	DebugMode: false,
}

// Sometimes users use config.yaml flag so /debug/vars doesn't have information about the
// value of the flags. Hence we dump conf options we care about to the conf map.
func setConfVar(conf Options) {
	newStr := func(s string) *expvar.String {
		v := new(expvar.String)
		v.Set(s)
		return v
	}

	newFloat := func(f float64) *expvar.Float {
		v := new(expvar.Float)
		v.Set(f)
		return v
	}

	newInt := func(i int) *expvar.Int {
		v := new(expvar.Int)
		v.Set(int64(i))
		return v
	}

	// Expvar doesn't have bool type so we use an int.
	newIntFromBool := func(b bool) *expvar.Int {
		v := new(expvar.Int)
		if b {
			v.Set(1)
		} else {
			v.Set(0)
		}
		return v
	}

	x.Conf.Set("posting_dir", newStr(conf.PostingDir))
	x.Conf.Set("posting_tables", newStr(conf.PostingTables))
	x.Conf.Set("wal_dir", newStr(conf.WALDir))
	x.Conf.Set("allotted_memory", newFloat(conf.AllottedMemory))
	x.Conf.Set("tracing", newFloat(conf.Tracing))
	x.Conf.Set("num_pending_proposals", newInt(conf.NumPendingProposals))
	x.Conf.Set("expand_edge", newIntFromBool(conf.ExpandEdge))
}

func SetConfiguration(newConfig Options) {
	newConfig.validate()
	setConfVar(newConfig)
	Config = newConfig

	posting.Config.Mu.Lock()
	posting.Config.AllottedMemory = Config.AllottedMemory
	posting.Config.Mu.Unlock()

	worker.Config.ExportPath = Config.ExportPath
	worker.Config.NumPendingProposals = Config.NumPendingProposals
	worker.Config.Tracing = Config.Tracing
	worker.Config.MyAddr = Config.MyAddr
	worker.Config.ZeroAddr = Config.ZeroAddr
	worker.Config.RaftId = Config.RaftId
	worker.Config.ExpandEdge = Config.ExpandEdge

	x.Config.DebugMode = Config.DebugMode
}

const MinAllottedMemory = 1024.0

func (o *Options) validate() {
	pd, err := filepath.Abs(o.PostingDir)
	x.Check(err)
	wd, err := filepath.Abs(o.WALDir)
	x.Check(err)
	x.AssertTruef(pd != wd, "Posting and WAL directory cannot be the same ('%s').", o.PostingDir)
	x.AssertTruefNoTrace(o.AllottedMemory != DefaultConfig.AllottedMemory,
		"LRU memory (--lru_mb) must be specified, with value greater than 1024 MB")
	x.AssertTruefNoTrace(o.AllottedMemory >= MinAllottedMemory,
		"LRU memory (--lru_mb) must be at least %.0f MB. Currently set to: %f",
		MinAllottedMemory, o.AllottedMemory)
}
