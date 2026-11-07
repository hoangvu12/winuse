package uia

// A failed child enumeration (reliability-council 05), offline: Children
// over a fake node and a fake IUIAutomationElementArray must return the
// children that did read and an error when the length or a child fails,
// so the walk marks the read partial; a real empty array stays complete.
// The fake COM objects are txtimeout_test.go's.

import (
	"slices"
	"syscall"
	"testing"
	"time"
)

// fakeArr is the fake array's state: its get_Length answer, its elements
// (a nil entry is a nil child) and the indexes whose GetElement fails.
var fakeArr struct {
	lengthHR HRESULT
	length   int32
	elements []comPtr
	failAt   []int
}

const fakeEFail = HRESULT(-0x7FFFBFFB) // 0x80004005 E_FAIL

var (
	fakeArrLength = syscall.NewCallback(func(this uintptr, out *int32) uintptr {
		if fakeArr.lengthHR != 0 {
			return uintptr(uint32(fakeArr.lengthHR))
		}
		*out = fakeArr.length
		return 0
	})
	fakeArrElement = syscall.NewCallback(func(this, i uintptr, out *comPtr) uintptr {
		if slices.Contains(fakeArr.failAt, int(i)) {
			hr := fakeEFail
			return uintptr(uint32(hr))
		}
		*out = fakeArr.elements[i]
		return 0
	})
	arrObj = newFakeCOM(map[uintptr]uintptr{arrGetLength: fakeArrLength, arrGetElement: fakeArrElement})
)

// parentNode is a node whose chunk succeeds with the fake array as its
// cached children.
var parentNode = func() *fakeCOM {
	f := newFakeCOM(map[uintptr]uintptr{
		elGetCachedChildren: syscall.NewCallback(func(this uintptr, out *comPtr) uintptr {
			*out = arrObj.ptr()
			return 0
		}),
	})
	f.vtbl[elBuildUpdatedCache] = syscall.NewCallback(func(this, req uintptr, out *comPtr) uintptr {
		*out = f.ptr()
		return 0
	})
	return f
}()

func TestChildrenReportsAFailedEnumeration(t *testing.T) {
	auto := newFakeCOM(map[uintptr]uintptr{uia2PutTransactionTimeout: fakeOK})
	src := &comSource{th: &thread{auto: auto.ptr()}}
	a, b, c := newFakeCOM(nil).ptr(), newFakeCOM(nil).ptr(), newFakeCOM(nil).ptr()

	cases := []struct {
		name     string
		lengthHR HRESULT
		length   int32
		elements []comPtr
		failAt   []int
		want     []comPtr
		wantErr  bool
	}{
		{name: "a failed length", lengthHR: fakeEFail, wantErr: true},
		{name: "a negative length", length: -1, wantErr: true},
		{name: "a failed middle child", length: 3, elements: []comPtr{a, b, c}, failAt: []int{1}, want: []comPtr{a, c}, wantErr: true},
		{name: "a nil child", length: 3, elements: []comPtr{a, nil, c}, want: []comPtr{a, c}, wantErr: true},
		{name: "a real empty array", length: 0},
		{name: "every child read", length: 3, elements: []comPtr{a, b, c}, want: []comPtr{a, b, c}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeArr.lengthHR, fakeArr.length, fakeArr.elements, fakeArr.failAt = tc.lengthHR, tc.length, tc.elements, tc.failAt
			kids, data, err := src.Children(parentNode.ptr(), time.Minute)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, want an error: %v", err, tc.wantErr)
			}
			if !slices.Equal(kids, tc.want) {
				t.Errorf("children = %v, want %v", kids, tc.want)
			}
			if len(data) != len(kids) {
				t.Errorf("%d NodeData for %d children", len(data), len(kids))
			}
		})
	}
}
