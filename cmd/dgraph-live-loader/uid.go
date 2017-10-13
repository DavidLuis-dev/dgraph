package main

import "fmt"

func xidKey(xid string) string {
	// Prefix to avoid key clashes with other data stored in badger.
	return "\x01" + xid
}

func (l *loader) NodeBlank(varname string) (uint64, error) {
	if len(varname) == 0 {
		uid, err := l.alloc.AllocateUid()
		if err != nil {
			return 0, err
		}
		return uid, nil
	}
	uid, _, err := l.alloc.AssignUid(xidKey("_:" + varname))
	return uid, err
}

// TODO - This should come from server.
func (l *loader) NodeXid(xid string, storeXid bool) (uint64, error) {
	if len(xid) == 0 {
		return 0, fmt.Errorf("Empty xid not allowed")
	}
	uid, _, err := l.alloc.AssignUid(xidKey(xid))
	// TODO(pawan) - Fix storing xids.
	//	if storeXid && isNew {
	//		e := n.Edge("xid")
	//		x.Check(e.SetValueString(xid))
	//		d.BatchSet(e)
	//	}
	return uid, err
}
