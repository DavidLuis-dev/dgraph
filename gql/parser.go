/*
 * Copyright 2015 Manish R Jain <manishrjain@gmaicom>
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

package gql

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/dgraph-io/dgraph/lex"
	"github.com/dgraph-io/dgraph/x"
)

var glog = x.Log("gql")

// GraphQuery stores the parsed Query in a tree format. This gets
// converted to internally used query.SubGraph before processing the query.
type GraphQuery struct {
	UID      uint64
	XID      string
	Attr     string
	Children []*GraphQuery
}

func run(l *lex.Lexer) {
	for state := lexText; state != nil; {
		state = state(l)
	}
	close(l.Items) // No more tokens.
}

func Parse(input string) (gq *GraphQuery, rerr error) {
	l := lex.NewLexer(input)
	go run(l)

	gq = nil
	for item := range l.Items {
		if item.Typ == itemText {
			continue
		}
		if item.Typ == itemOpType {
			if item.Val == "mutation" {
				return nil, errors.New("Mutations not supported")
			}
		}
		if item.Typ == itemLeftCurl {
			if gq == nil {
				gq, rerr = getRoot(l)
				if rerr != nil {
					x.Err(glog, rerr).Error("While retrieving subgraph root")
					return nil, rerr
				}
			} else {
				if err := godeep(l, gq); err != nil {
					return gq, err
				}
			}
		}
	}
	return gq, nil
}

func getRoot(l *lex.Lexer) (gq *GraphQuery, rerr error) {
	item := <-l.Items
	if item.Typ != itemName {
		return nil, fmt.Errorf("Expected some name. Got: %v", item)
	}
	// ignore itemName for now.
	item = <-l.Items
	if item.Typ != itemLeftRound {
		return nil, fmt.Errorf("Expected variable start. Got: %v", item)
	}

	var uid uint64
	var xid string
	for {
		var key, val string
		// Get key or close bracket
		item = <-l.Items
		if item.Typ == itemArgName {
			key = item.Val
		} else if item.Typ == itemRightRound {
			break
		} else {
			return nil, fmt.Errorf("Expecting argument name. Got: %v", item)
		}

		// Get corresponding value.
		item = <-l.Items
		if item.Typ == itemArgVal {
			val = item.Val
		} else {
			return nil, fmt.Errorf("Expecting argument va Got: %v", item)
		}

		if key == "_uid_" {
			uid, rerr = strconv.ParseUint(val, 0, 64)
			if rerr != nil {
				return nil, rerr
			}
		} else if key == "_xid_" {
			xid = val
		} else {
			return nil, fmt.Errorf("Expecting _uid_ or _xid_. Got: %v", item)
		}
	}
	if item.Typ != itemRightRound {
		return nil, fmt.Errorf("Unexpected token. Got: %v", item)
	}
	gq = new(GraphQuery)
	gq.UID = uid
	gq.XID = xid
	return gq, nil
}

func godeep(l *lex.Lexer, gq *GraphQuery) error {
	curp := gq // Used to track current node, for nesting.
	for item := range l.Items {
		if item.Typ == lex.ItemError {
			return errors.New(item.Val)

		} else if item.Typ == lex.ItemEOF {
			return nil

		} else if item.Typ == itemName {
			child := new(GraphQuery)
			child.Attr = item.Val
			gq.Children = append(gq.Children, child)
			curp = child

		} else if item.Typ == itemLeftCurl {
			if err := godeep(l, curp); err != nil {
				return err
			}

		} else if item.Typ == itemRightCurl {
			return nil

		} else if item.Typ == itemLeftRound {
			// absorb all these, we don't use them right now.
			for ti := range l.Items {
				if ti.Typ == itemRightRound || ti.Typ == lex.ItemEOF {
					return nil
				}
			}
		}
	}
	return nil
}
