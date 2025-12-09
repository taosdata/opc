package opc

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/stretchr/testify/assert"
)

type exceptionInfo struct {
	wCode             uint16
	wReserved         uint16
	bstrSource        *uint16
	bstrDescription   *uint16
	bstrHelpFile      *uint16
	dwHelpContext     uint32
	pvReserved        uintptr
	pfnDeferredFillIn uintptr
	scode             uint32

	rendered    bool
	source      string
	description string
	helpFile    string
}

func TestTryGetOPCError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name    string
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "nil error",
			args: args{
				err: nil,
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Nil(t, err, i...)
			},
		},
		{
			name: "non OPC error",
			args: args{
				err: fmt.Errorf("some error"),
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.ErrorContains(t, err, "some error", i...)
			},
		},
		{
			name: "OLE error",
			args: args{
				err: ole.NewError(0x80004003),
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				opcErr := TryGetOPCError(ole.NewError(0x80004003))
				if opcErr == nil {
					return assert.Fail(t, "expected OPC error, got nil", i...)
				}
				return assert.Equal(t, uintptr(0x80004003), opcErr.(*ole.OleError).Code(), i...)
			},
		},
		{
			name: "OLE Exception error",
			args: args{
				err: ole.NewErrorWithSubError(0x80004003, "Test Exception", ole.EXCEPINFO{}),
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				opcErr := TryGetOPCError(ole.NewErrorWithSubError(0x80004003, "Test Exception", ole.EXCEPINFO{}))
				if opcErr == nil {
					return assert.Fail(t, "expected OPC error, got nil", i...)
				}
				return assert.Equal(t, opcErr, err)
			},
		},
		{
			name: "suberror not OLE error",
			args: args{
				err: ole.NewErrorWithSubError(0x80004003, "Test Exception", fmt.Errorf("some suberror")),
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				opcErr := TryGetOPCError(ole.NewErrorWithSubError(0x80004003, "Test Exception", fmt.Errorf("some suberror")))
				if opcErr == nil {
					return assert.Fail(t, "expected OPC error, got nil", i...)
				}
				return assert.Equal(t, opcErr, err)
			},
		},
		{
			name: "OPC error",
			args: args{
				err: ole.NewErrorWithSubError(0x80004003, "Test Exception", *(*ole.EXCEPINFO)(unsafe.Pointer(&exceptionInfo{
					scode: 0xC0040007,
				}))),
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				want := NewOPCError(0xC0040007)
				return assert.Equal(t, want, err, i)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.wantErr(t, TryGetOPCError(tt.args.err), fmt.Sprintf("TryGetOPCError(%v)", tt.args.err))
		})
	}
}

func TestOPCError(t *testing.T) {
	err := NewOPCError(0xC0040007)
	assert.Equal(t, "OPC ERROR code:0xC0040007, error:The item ID is not defined in the server address space (on add or validate) or no longer exists in the server address space (for read or write).", err.Error())
	err = NewOPCError(0x800706BA)
	assert.Equal(t, "OPC ERROR code:0x800706BA, error:Unknown error, may be system error: The RPC server is unavailable.", err.Error())
	err = NewOPCError(0x12345678)
	assert.Equal(t, "OPC ERROR code:0x12345678, error:Unknown error, may be system error: winapi error #305419896", err.Error())
}
