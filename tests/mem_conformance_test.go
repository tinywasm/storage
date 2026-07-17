package tests

import (
	"testing"

	"github.com/tinywasm/model"
	"github.com/tinywasm/storage"
	"github.com/tinywasm/storage/conformance"
	"github.com/tinywasm/storage/mem"
)

func TestMemConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New: func(t *testing.T, models ...model.Model) storage.Conn {
			return mem.New()
		},
	})
}
