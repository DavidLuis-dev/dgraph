/*
 * Copyright 2015 Manish R Jain <manishrjain@gmail.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * 		http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package uid

import (
	"bytes"
	"errors"
	"math"
	"time"

	"github.com/dgraph-io/dgraph/posting"
	"github.com/dgraph-io/dgraph/posting/types"
	"github.com/dgraph-io/dgraph/x"
	"github.com/dgryski/go-farm"
)

var log = x.Log("uid")

func allocateNew(xid string) (uid uint64, rerr error) {
	for sp := ""; ; sp += " " {
		txid := xid + sp
		uid = farm.Fingerprint64([]byte(txid)) // Generate from hash.
		log.WithField("txid", txid).WithField("uid", uid).Debug("Generated")
		if uid == math.MaxUint64 {
			log.Debug("Hit uint64max while generating fingerprint. Ignoring...")
			continue
		}

		// Check if this uid has already been allocated.
		key := posting.Key(uid, "_xid_") // uid -> "_xid_" -> xid
		pl := posting.Get(key)

		if pl.Length() > 0 {
			// Something already present here.
			var p types.Posting
			pl.Get(&p, 0)

			var tmp interface{}
			posting.ParseValue(&tmp, p.ValueBytes())
			log.Debug("Found existing xid: [%q]. Continuing...", tmp.(string))
			continue
		}

		// Uid hasn't been assigned yet.
		t := x.DirectedEdge{
			Value:     xid, // not txid
			Source:    "_assigner_",
			Timestamp: time.Now(),
		}
		rerr = pl.AddMutation(t, posting.Set)
		if rerr != nil {
			x.Err(log, rerr).Error("While adding mutation")
		}
		return uid, rerr
	}
	return 0, errors.New("Some unhandled route lead me here." +
		" Wake the stupid developer up.")
}

func stringKey(xid string) []byte {
	buf := new(bytes.Buffer)
	buf.WriteString("_uid_")
	buf.WriteString("|")
	buf.WriteString(xid)
	return buf.Bytes()
}

// TODO: Currently one posting list is modified after another, without
func GetOrAssign(xid string) (uid uint64, rerr error) {
	key := stringKey(xid)
	pl := posting.Get(key)
	if pl.Length() == 0 {
		// No current id exists. Create one.
		uid, err := allocateNew(xid)
		if err != nil {
			return 0, err
		}
		t := x.DirectedEdge{
			ValueId:   uid,
			Source:    "_assigner_",
			Timestamp: time.Now(),
		}
		rerr = pl.AddMutation(t, posting.Set)
		return uid, rerr

	} else if pl.Length() > 1 {
		log.Fatalf("We shouldn't have more than 1 uid for xid: %v\n", xid)

	} else {
		// We found one posting.
		var p types.Posting
		if ok := pl.Get(&p, 0); !ok {
			return 0, errors.New("While retrieving entry from posting list")
		}
		return p.Uid(), nil
	}
	return 0, errors.New("Some unhandled route lead me here." +
		" Wake the stupid developer up.")
}

func ExternalId(uid uint64) (xid string, rerr error) {
	key := posting.Key(uid, "_xid_") // uid -> "_xid_" -> xid
	pl := posting.Get(key)
	if pl.Length() == 0 {
		return "", errors.New("NO external id")
	}

	if pl.Length() > 1 {
		log.WithField("uid", uid).Fatal("This shouldn't be happening.")
		return "", errors.New("Multiple external ids for this uid.")
	}

	var p types.Posting
	if ok := pl.Get(&p, 0); !ok {
		log.WithField("uid", uid).Error("While retrieving posting")
		return "", errors.New("While retrieving posting")
	}

	if p.Uid() != math.MaxUint64 {
		log.WithField("uid", uid).Fatal("Value uid must be MaxUint64.")
	}
	var t interface{}
	rerr = posting.ParseValue(&t, p.ValueBytes())
	xid = t.(string)
	return xid, rerr
}
