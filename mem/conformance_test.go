package mem

import (
	"testing"

	"github.com/tinywasm/db"
	"github.com/tinywasm/db/conformance"
	"github.com/tinywasm/model"
)

func TestMemConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New: func(t *testing.T, models ...model.Model) db.Conn {
			return New()
		},
	})
}
