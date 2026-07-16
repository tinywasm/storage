package tests

import (
	"testing"

	"github.com/tinywasm/model"
	"github.com/tinywasm/storage/conformance"
)

type dummyWriter struct {
	model.FieldWriter
	strings map[string]string
	ints    map[string]int64
	bools   map[string]bool
}

func (d *dummyWriter) String(name, val string) {
	d.strings[name] = val
}

func (d *dummyWriter) Int(name string, val int64) {
	d.ints[name] = val
}

func (d *dummyWriter) Bool(name string, val bool) {
	d.bools[name] = val
}

type dummyReader struct {
	model.FieldReader
	strings map[string]string
	ints    map[string]int64
	bools   map[string]bool
}

func (d *dummyReader) String(name string) (string, bool) {
	v, ok := d.strings[name]
	return v, ok
}

func (d *dummyReader) Int(name string) (int64, bool) {
	v, ok := d.ints[name]
	return v, ok
}

func (d *dummyReader) Bool(name string) (bool, bool) {
	v, ok := d.bools[name]
	return v, ok
}

func TestWidgetModelExtra(t *testing.T) {
	w := &conformance.Widget{
		Id:     "w1",
		Name:   "widget1",
		Qty:    10,
		Active: true,
	}

	if w.IsNil() {
		t.Error("expected IsNil to be false")
	}

	var nilWidget *conformance.Widget
	if !nilWidget.IsNil() {
		t.Error("expected nil widget to be IsNil true")
	}

	writer := &dummyWriter{
		strings: make(map[string]string),
		ints:    make(map[string]int64),
		bools:   make(map[string]bool),
	}
	w.EncodeFields(writer)

	if writer.strings["id"] != "w1" || writer.strings["name"] != "widget1" || writer.ints["qty"] != 10 || !writer.bools["active"] {
		t.Errorf("EncodeFields didn't write fields correctly: %+v", writer)
	}

	reader := &dummyReader{
		strings: map[string]string{"id": "w2", "name": "widget2"},
		ints:    map[string]int64{"qty": 20},
		bools:   map[string]bool{"active": false},
	}

	var w2 conformance.Widget
	w2.DecodeFields(reader)

	if w2.Id != "w2" || w2.Name != "widget2" || w2.Qty != 20 || w2.Active {
		t.Errorf("DecodeFields didn't read fields correctly: %+v", w2)
	}
}
