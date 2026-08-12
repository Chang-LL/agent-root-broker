package homeaccess

import (
	"reflect"
	"testing"
)

func TestACLGrantRoundTripAndRevoke(t *testing.T) {
	value := baseACL(0o750)
	value.grantUser(1001, 7)
	if !value.userHas(1001, 7) {
		t.Fatal("granted user lacks effective rwx")
	}
	encoded := value.encode()
	decoded, err := parseACL(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.userHas(1001, 7) {
		t.Fatal("round-tripped user lacks effective rwx")
	}
	if !decoded.revokeUser(1001) || decoded.userHas(1001, 1) {
		t.Fatal("revoke did not remove user")
	}
	if !reflect.DeepEqual(decoded.encode(), baseACL(0o750).encode()) {
		t.Fatalf("minimal ACL was not restored: %#v", decoded.entries)
	}
}

func TestACLRevokeRecalculatesRemainingMask(t *testing.T) {
	value := baseACL(0o750)
	value.grantUser(2000, 4)
	value.grantUser(1001, 7)
	if !value.revokeUser(1001) {
		t.Fatal("user was not removed")
	}
	if !value.userHas(2000, 4) || value.userHas(2000, 2) {
		t.Fatal("remaining ACL mask is incorrect")
	}
}

func TestACLGrantDoesNotUnmaskExistingPrincipals(t *testing.T) {
	value := aclValue{entries: []aclEntry{
		{tag: tagUserObj, perm: 7, id: undefinedID},
		{tag: tagUser, perm: 7, id: 2000},
		{tag: tagGroupObj, perm: 7, id: undefinedID},
		{tag: tagMask, perm: 4, id: undefinedID},
		{tag: tagOther, perm: 0, id: undefinedID},
	}}
	value.grantUser(1001, 7)
	if !value.userHas(1001, 7) {
		t.Fatal("new user lacks requested permissions")
	}
	if !value.userHas(2000, 4) || value.userHas(2000, 2) {
		t.Fatal("grant broadened an existing user's effective permissions")
	}
	if !value.revokeUser(1001) || !value.userHas(2000, 4) || value.userHas(2000, 2) {
		t.Fatal("revoke did not preserve the existing user's effective permissions")
	}
}

func TestManageValidatesAndDelegates(t *testing.T) {
	var action, home string
	var uid uint32
	manager := &Manager{
		lookupUID: func(string) (uint32, error) { return 1001, nil },
		operation: func(gotAction, gotHome string, gotUID uint32) (string, error) {
			action, home, uid = gotAction, gotHome, gotUID
			return "enabled", nil
		},
	}
	result, err := manager.Manage("grant", "/home/person", "grok-agent")
	if err != nil || result.State != "enabled" || action != "grant" || home != "/home/person" || uid != 1001 {
		t.Fatalf("result=%+v action=%q home=%q uid=%d err=%v", result, action, home, uid, err)
	}
	for _, test := range []struct{ action, home, user string }{
		{"grant", "/", "grok-agent"}, {"grant", "/home/person", "-bad"}, {"bad", "/home/person", "grok-agent"},
	} {
		if _, err := manager.Manage(test.action, test.home, test.user); err == nil {
			t.Fatalf("accepted action=%q home=%q user=%q", test.action, test.home, test.user)
		}
	}
}
