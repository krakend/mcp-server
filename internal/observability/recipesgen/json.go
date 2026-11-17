package recipesgen

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// KV is one key of an ordered JSON object.
type KV struct {
	Key   string
	Value any
}

// Obj is a JSON object that keeps its key order, so the generated file is stable and diffs stay
// readable.
type Obj []KV

// Set replaces the value of key, or appends it.
func (o Obj) Set(key string, v any) Obj {
	for i := range o {
		if o[i].Key == key {
			o[i].Value = v
			return o
		}
	}
	return append(o, KV{key, v})
}

// Get returns the value of key, or nil.
func (o Obj) Get(key string) any {
	for _, kv := range o {
		if kv.Key == key {
			return kv.Value
		}
	}
	return nil
}

// Float is a number written the way Python writes floats (1.0, 0.05, 1e-05), so values parsed
// from "1s" stay 1.0 and don't turn into the integer 1.
type Float float64

// Marshal writes v as JSON indented by 2 spaces, non-ASCII escaped as \uXXXX: the output of
// Python's json.dumps(v, indent=2), which earlier versions of this file were generated with.
func Marshal(v any) []byte {
	var b strings.Builder
	write(&b, v, 0)
	return []byte(b.String())
}

func write(b *strings.Builder, v any, depth int) {
	pad := func(d int) string { return "\n" + strings.Repeat("  ", d) }
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case Float:
		b.WriteString(pyFloat(float64(x)))
	case string:
		writeString(b, x)
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		write(b, items, depth)
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[")
		for i, item := range x {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(pad(depth + 1))
			write(b, item, depth+1)
		}
		b.WriteString(pad(depth) + "]")
	case Obj:
		if len(x) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{")
		for i, kv := range x {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(pad(depth + 1))
			writeString(b, kv.Key)
			b.WriteString(": ")
			write(b, kv.Value, depth+1)
		}
		b.WriteString(pad(depth) + "}")
	default:
		panic(fmt.Sprintf("recipesgen: can't write %T", v))
	}
}

func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7e:
				b.WriteRune(r)
			case r > 0xffff:
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				fmt.Fprintf(b, `\u%04x`, r)
			}
		}
	}
	b.WriteByte('"')
}

// pyFloat formats like Python's repr(float): shortest round-trip digits, ".0" on whole numbers,
// exponent form below 1e-4 and from 1e16.
func pyFloat(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		panic("recipesgen: non-finite float")
	}
	if f != 0 && (math.Abs(f) < 1e-4 || math.Abs(f) >= 1e16) {
		s := strconv.FormatFloat(f, 'e', -1, 64) // 1e-05 in Python, 1e-05 here
		return s
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".") {
		s += ".0"
	}
	return s
}
