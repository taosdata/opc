package opc

import (
	"fmt"
	"syscall"

	"github.com/go-ole/go-ole"
)

var (
	OPCInvalidHandle   = uint32(0xC0040001)
	OPCBadType         = uint32(0xC0040004)
	OPCPublic          = uint32(0xC0040005)
	OPCBadRights       = uint32(0xC0040006)
	OPCUnknownItemID   = uint32(0xC0040007)
	OPCInvalidItemID   = uint32(0xC0040008)
	OPCInvalidFilter   = uint32(0xC0040009)
	OPCUnknownPath     = uint32(0xC004000A)
	OPCRange           = uint32(0xC004000B)
	OPCDuplicateName   = uint32(0xC004000C)
	OPCUnsupportedRate = uint32(0x0004000D)
	OPCClamp           = uint32(0x0004000E)
	OPCInuse           = uint32(0x0004000F)
	OPCInvalidConfig   = uint32(0xC0040010)
	OPCNotFound        = uint32(0xC0040011)
	OPCInvalidPID      = uint32(0xC0040203)
)

var OPCErrorMap = map[uint32]string{
	OPCInvalidHandle:   "The value of the handle is invalid",
	OPCBadType:         "The server cannot convert the data between the specified format/ requested data type and the canonical data type",
	OPCPublic:          "The requested operation cannot be done on a public group",
	OPCBadRights:       "The Items AccessRights do not allow the operation",
	OPCUnknownItemID:   "The item ID is not defined in the server address space (on add or validate) or no longer exists in the server address space (for read or write).",
	OPCInvalidItemID:   "The item ID doesn't conform to the server's syntax",
	OPCInvalidFilter:   "The filter string was not valid",
	OPCUnknownPath:     "The item's access path is not known to the server",
	OPCRange:           "The value was out of range",
	OPCDuplicateName:   "Duplicate name not allowed",
	OPCUnsupportedRate: "The server does not support the requested data rate but will use the closest available rate",
	OPCClamp:           "A value passed to WRITE was accepted but the output was clamped",
	OPCInuse:           "The operation cannot be performed because the object is bering referenced",
	OPCInvalidConfig:   "The server's configuration file is an invalid format",
	OPCNotFound:        "Requested Object was not found",
	OPCInvalidPID:      "The passed property ID is not valid for the item",
}

type OPCError struct {
	Code uint32
	Msg  string
}

func (e *OPCError) Error() string {
	return fmt.Sprintf("OPC ERROR code:0x%X, error:%s", e.Code, e.Msg)
}

func NewOPCError(code uint32) error {
	if msg, ok := OPCErrorMap[code]; ok {
		return &OPCError{
			Code: code,
			Msg:  msg,
		}
	}
	return &OPCError{
		Code: code,
		Msg:  fmt.Sprintf("Unknown error, may be system error: %s", syscall.Errno(code).Error()),
	}
}

func TryGetOPCError(err error) error {
	oleErr, is := err.(*ole.OleError)
	if !is {
		return err
	}
	subErr := oleErr.SubError()
	if subErr == nil {
		return err
	}

	exceptInfo, is := subErr.(ole.EXCEPINFO)
	if !is {
		return err
	}
	code := uint32(exceptInfo.WCode())
	if code == 0 {
		code = exceptInfo.SCODE()
	}
	if code == 0 {
		return err
	}
	return NewOPCError(code)
}
