/*
 * Copyright (C) 2017 Dgraph Labs, Inc. and Contributors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package client

import (
	"context"
	"fmt"
	"math/rand"

	"google.golang.org/grpc"

	"github.com/dgraph-io/dgraph/protos"
	"github.com/dgraph-io/dgraph/types"
	"github.com/dgraph-io/dgraph/x"
)

type Dgraph struct {
	dc []protos.DgraphClient
}

// NewDgraphClient creates a new Dgraph for interacting with the Dgraph store connected to in
// conns.
// The client can be backed by multiple connections (to the same server, or multiple servers in a
// cluster).
//
// A single client is thread safe for sharing with multiple go routines (though a single Req
// should not be shared unless the go routines negotiate exclusive assess to the Req functions).
func NewDgraphClient(conns []*grpc.ClientConn) *Dgraph {
	var clients []protos.DgraphClient
	for _, conn := range conns {
		client := protos.NewDgraphClient(conn)
		clients = append(clients, client)
	}
	return NewClient(clients)
}

// TODO(tzdybal) - hide this function from users
func NewClient(clients []protos.DgraphClient) *Dgraph {
	d := &Dgraph{
		dc: clients,
	}

	return d
}

// DropAll deletes all edges and schema from Dgraph.
func (d *Dgraph) DropAll(ctx context.Context) error {
	req := &Req{
		gr: protos.Request{
			Mutation: &protos.Mutation{DropAll: true},
		},
	}

	_, err := d.dc[rand.Intn(len(d.dc))].Run(ctx, &req.gr)
	return err
}

func (d *Dgraph) CheckSchema(schema *protos.SchemaUpdate) error {
	if len(schema.Predicate) == 0 {
		return x.Errorf("No predicate specified for schemaUpdate")
	}
	typ := types.TypeID(schema.ValueType)
	if typ == types.UidID && schema.Directive == protos.SchemaUpdate_INDEX {
		// index on uid type
		return x.Errorf("Index not allowed on predicate of type uid on predicate %s",
			schema.Predicate)
	} else if typ != types.UidID && schema.Directive == protos.SchemaUpdate_REVERSE {
		// reverse on non-uid type
		return x.Errorf("Cannot reverse for non-uid type on predicate %s", schema.Predicate)
	}
	return nil
}

func (d *Dgraph) SetSchemaBlocking(ctx context.Context, updates []*protos.SchemaUpdate) error {
	for _, s := range updates {
		if err := d.CheckSchema(s); err != nil {
			return err
		}
		req := new(Req)
		che := make(chan error, 1)
		req.AddSchema(s)
		go func() {
			if _, err := d.dc[rand.Intn(len(d.dc))].Run(ctx, &req.gr); err != nil {
				che <- err
				return
			}
			che <- nil
		}()

		// blocking wait until schema is applied
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-che:
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Run runs the request in req and returns with the completed response from the server.  Calling
// Run has no effect on batched mutations.
//
// Mutations in the request are run before a query --- except when query variables link the
// mutation and query (see for example NodeUidVar) when the query is run first.
//
// Run returns a protos.Response which has the following fields
//
// - L : Latency information
//
// - Schema : Result of a schema query
//
// - AssignedUids : a map[string]uint64 of blank node name to assigned UID (if the query string
// contained a mutation with blank nodes)
//
// - N : Slice of *protos.Node returned by the query (Note: protos.Node not client.Node).
//
// There is an N[i], with Attribute "_root_", for each named query block in the query added to req.
// The N[i] also have a slice of nodes, N[i].Children each with Attribute equal to the query name,
// for every answer to that query block.  From there, the Children represent nested blocks in the
// query, the Attribute is the edge followed and the Properties are the scalar edges.
//
// Print a response with
// 	"github.com/gogo/protobuf/proto"
// 	...
// 	req.SetQuery(`{
// 		friends(func: eq(name, "Alex")) {
//			name
//			friend {
// 				name
//			}
//		}
//	}`)
// 	...
// 	resp, err := dgraphClient.Run(context.Background(), &req)
// 	fmt.Printf("%+v\n", proto.MarshalTextString(resp))
// Outputs
//	n: <
//	  attribute: "_root_"
//	  children: <
//	    attribute: "friends"
//	    properties: <
//	      prop: "name"
//	      value: <
//	        str_val: "Alex"
//	      >
//	    >
//	    children: <
//	      attribute: "friend"
//	      properties: <
//	        prop: "name"
//	        value: <
//	          str_val: "Chris"
//	        >
//	      >
//	    >
//	...
//
// It's often easier to unpack directly into a struct with Unmarshal, than to
// step through the response.
func (d *Dgraph) Run(ctx context.Context, req *Req) (*protos.Response, error) {
	res, err := d.dc[rand.Intn(len(d.dc))].Run(ctx, &req.gr)
	if err == nil {
		req = &Req{}
	}
	return res, err
}

// CheckVersion checks if the version of dgraph and dgraph-live-loader are the same.  If either the
// versions don't match or the version information could not be obtained an error message is
// printed.
func (d *Dgraph) CheckVersion(ctx context.Context) {
	v, err := d.dc[rand.Intn(len(d.dc))].CheckVersion(ctx, &protos.Check{})
	if err != nil {
		fmt.Printf(`Could not fetch version information from Dgraph. Got err: %v.`, err)
	} else {
		version := x.Version()
		if version != "" && v.Tag != "" && version != v.Tag {
			fmt.Printf(`
Dgraph server: %v, loader: %v dont match.
You can get the latest version from https://docs.dgraph.io
`, v.Tag, version)
		}
	}
}

func (d *Dgraph) AnyClient() protos.DgraphClient {
	return d.dc[rand.Intn(len(d.dc))]
}
