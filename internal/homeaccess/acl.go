package homeaccess

import (
	"encoding/binary"
	"fmt"
	"sort"
)

const (
	aclVersion  = 2
	tagUserObj  = 0x01
	tagUser     = 0x02
	tagGroupObj = 0x04
	tagGroup    = 0x08
	tagMask     = 0x10
	tagOther    = 0x20
	undefinedID = ^uint32(0)
)

type aclEntry struct {
	tag  uint16
	perm uint16
	id   uint32
}

type aclValue struct {
	entries []aclEntry
}

func parseACL(data []byte) (aclValue, error) {
	if len(data) < 4 || (len(data)-4)%8 != 0 || binary.LittleEndian.Uint32(data[:4]) != aclVersion {
		return aclValue{}, fmt.Errorf("invalid POSIX ACL xattr")
	}
	result := aclValue{entries: make([]aclEntry, 0, (len(data)-4)/8)}
	for offset := 4; offset < len(data); offset += 8 {
		entry := aclEntry{
			tag:  binary.LittleEndian.Uint16(data[offset : offset+2]),
			perm: binary.LittleEndian.Uint16(data[offset+2 : offset+4]),
			id:   binary.LittleEndian.Uint32(data[offset+4 : offset+8]),
		}
		if entry.perm&^uint16(7) != 0 {
			return aclValue{}, fmt.Errorf("invalid POSIX ACL permissions")
		}
		result.entries = append(result.entries, entry)
	}
	return result, nil
}

func (a aclValue) encode() []byte {
	entries := append([]aclEntry(nil), a.entries...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].tag != entries[j].tag {
			return entries[i].tag < entries[j].tag
		}
		return entries[i].id < entries[j].id
	})
	result := make([]byte, 4+8*len(entries))
	binary.LittleEndian.PutUint32(result[:4], aclVersion)
	for index, entry := range entries {
		offset := 4 + index*8
		binary.LittleEndian.PutUint16(result[offset:offset+2], entry.tag)
		binary.LittleEndian.PutUint16(result[offset+2:offset+4], entry.perm)
		binary.LittleEndian.PutUint32(result[offset+4:offset+8], entry.id)
	}
	return result
}

func baseACL(mode uint32) aclValue {
	return aclValue{entries: []aclEntry{
		{tag: tagUserObj, perm: uint16(mode >> 6 & 7), id: undefinedID},
		{tag: tagGroupObj, perm: uint16(mode >> 3 & 7), id: undefinedID},
		{tag: tagOther, perm: uint16(mode & 7), id: undefinedID},
	}}
}

func (a aclValue) base() aclValue {
	result := aclValue{}
	for _, entry := range a.entries {
		if entry.tag == tagUserObj || entry.tag == tagGroupObj || entry.tag == tagOther {
			result.entries = append(result.entries, entry)
		}
	}
	return result
}

func (a *aclValue) grantUser(uid uint32, perm uint16) {
	// POSIX ACLs share one mask across the owning group and every named entry.
	// Collapse existing entries to their current effective permissions before
	// expanding that mask, so this grant cannot accidentally unmask access for
	// an unrelated existing ACL principal.
	mask := a.effectiveMask()
	for index := range a.entries {
		switch a.entries[index].tag {
		case tagGroupObj, tagUser, tagGroup:
			a.entries[index].perm &= mask
		}
	}
	found := false
	for index := range a.entries {
		if a.entries[index].tag == tagUser && a.entries[index].id == uid {
			a.entries[index].perm = perm
			found = true
		}
	}
	if !found {
		a.entries = append(a.entries, aclEntry{tag: tagUser, perm: perm, id: uid})
	}
	a.recalculateMask()
}

func (a *aclValue) revokeUser(uid uint32) bool {
	result := make([]aclEntry, 0, len(a.entries))
	removed := false
	for _, entry := range a.entries {
		if entry.tag == tagUser && entry.id == uid {
			removed = true
			continue
		}
		result = append(result, entry)
	}
	a.entries = result
	if !removed {
		return false
	}
	if !a.hasNamedEntries() {
		withoutMask := make([]aclEntry, 0, len(a.entries))
		for _, entry := range a.entries {
			if entry.tag != tagMask {
				withoutMask = append(withoutMask, entry)
			}
		}
		a.entries = withoutMask
	} else {
		a.recalculateMask()
	}
	return true
}

func (a aclValue) userHas(uid uint32, wanted uint16) bool {
	mask := a.effectiveMask()
	for _, entry := range a.entries {
		if entry.tag == tagUser && entry.id == uid {
			return entry.perm&mask&wanted == wanted
		}
	}
	return false
}

func (a aclValue) effectiveMask() uint16 {
	for _, entry := range a.entries {
		if entry.tag == tagMask {
			return entry.perm
		}
	}
	return 7
}

func (a aclValue) hasNamedEntries() bool {
	for _, entry := range a.entries {
		if entry.tag == tagUser || entry.tag == tagGroup {
			return true
		}
	}
	return false
}

func (a *aclValue) recalculateMask() {
	mask := uint16(0)
	maskIndex := -1
	for index, entry := range a.entries {
		switch entry.tag {
		case tagGroupObj, tagUser, tagGroup:
			mask |= entry.perm
		case tagMask:
			maskIndex = index
		}
	}
	if maskIndex >= 0 {
		a.entries[maskIndex].perm = mask
	} else {
		a.entries = append(a.entries, aclEntry{tag: tagMask, perm: mask, id: undefinedID})
	}
}
