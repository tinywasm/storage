package mem

import (
	"testing"

	"github.com/tinywasm/model"
	"github.com/tinywasm/storage"
	"github.com/tinywasm/storage/conformance"
)

func TestMemConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New: func(t *testing.T, models ...model.Model) storage.Conn {
			return New()
		},
	})
}
