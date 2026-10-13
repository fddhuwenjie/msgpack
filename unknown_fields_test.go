package msgpack_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// unknownFieldPrefix is the recognizable strict-mode error prefix that
// existing callers depend on.
const unknownFieldPrefix = "msgpack: unknown field "

//------------------------------------------------------------------------------
// Destination types used by the strict decoding tests.
//------------------------------------------------------------------------------

type strictLeaf struct {
	Foo string `msgpack:"foo"`
}

type strictPtrWrap struct {
	Child *strictLeaf
}

type strictRenameWrap struct {
	Renamed strictLeaf `msgpack:"custom_name"`
}

type strictInner struct {
	Name string `msgpack:"name"`
	Leaf strictLeaf
}

type strictOuter struct {
	Title string `msgpack:"title"`
	Inner strictInner
}

type strictEmbed struct {
	Embedded string
	Leaf     strictLeaf
}

// strictInlineOuter flattens strictEmbed: its fields must not get an
// extra path segment through the embedded struct.
type strictInlineOuter struct {
	strictEmbed
}

type strictAsArray struct {
	_msgpack struct{} `msgpack:",as_array"`

	Name string
	Leaf strictLeaf
}

type strictArrWrap struct {
	Arr strictAsArray `msgpack:"arr"`
}

type strictItems struct {
	Items []strictLeaf
}

type strictFixedItems struct {
	Items [2]strictLeaf
}

type strictWithMap struct {
	M map[string]interface{}
}

type strictJSONChild struct {
	Value string `json:"value"`
}

type strictJSONParent struct {
	Kid strictJSONChild
}

//------------------------------------------------------------------------------
// Wire types carrying extra (unknown) keys.
//------------------------------------------------------------------------------

type wireLeaf struct {
	Foo   string `msgpack:"foo"`
	Bogus string `msgpack:"bogus,omitempty"`
}

// wireLeafUnknownFirst puts the unknown key before the known one to make
// sure known fields are still written after the unknown value is skipped.
type wireLeafUnknownFirst struct {
	Bogus string
	Foo   string `msgpack:"foo"`
}

type wirePtrWrap struct {
	Child wireLeaf
}

type wireRenameWrap struct {
	Renamed wireLeaf `msgpack:"custom_name"`
}

type wireInnerDeep struct {
	Name string `msgpack:"name"`
	Leaf wireLeaf
}

type wireOuterDeep struct {
	Inner wireInnerDeep
}

type wireInner struct {
	Name  string `msgpack:"name"`
	Bogus string `msgpack:"bogus"`
	Leaf  wireLeaf
}

type wireOuter struct {
	Title string `msgpack:"title"`
	Inner wireInner
}

type wireEmbed struct {
	Embedded string
	Bogus    string
	Leaf     wireLeaf
}

type wireEmbedDeep struct {
	Embedded string
	Leaf     wireLeaf
}

type wireAsArray struct {
	_msgpack struct{} `msgpack:",as_array"`

	Name string
	Leaf wireLeaf
}

type wireArrWrap struct {
	Arr wireAsArray `msgpack:"arr"`
}

type wireItems struct {
	Items []wireLeaf
}

type wireFixedItems struct {
	Items [2]wireLeaf
}

type wireJSONChild struct {
	Value string `json:"value"`
	Bad   string `json:"bad"`
}

type wireJSONParent struct {
	Kid wireJSONChild
}

//------------------------------------------------------------------------------
// Helpers.
//------------------------------------------------------------------------------

func mustMarshalStrict(t *testing.T, v interface{}) []byte {
	t.Helper()

	b, err := msgpack.Marshal(v)
	require.NoError(t, err)
	return b
}

func newStrictDecoder(data []byte) *msgpack.Decoder {
	dec := msgpack.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields(true)
	return dec
}

type unknownFieldCall struct {
	path string
	name string
}

func recordUnknownFields(calls *[]unknownFieldCall) func(*msgpack.Decoder, string) error {
	return func(d *msgpack.Decoder, name string) error {
		*calls = append(*calls, unknownFieldCall{path: d.Path(), name: name})
		return nil
	}
}

//------------------------------------------------------------------------------
// Full logical path in the strict-mode error.
//------------------------------------------------------------------------------

func TestStrictUnknownFieldPath(t *testing.T) {
	tests := []struct {
		name       string
		wire       interface{}
		newTarget  func() interface{}
		wantPath   string
		customTag  string
		encodeJSON bool
	}{
		{
			name:      "root field",
			wire:      wireLeaf{Foo: "x", Bogus: "y"},
			newTarget: func() interface{} { return &strictLeaf{} },
			wantPath:  `bogus`,
		},
		{
			name:      "field behind pointer",
			wire:      wirePtrWrap{Child: wireLeaf{Foo: "x", Bogus: "y"}},
			newTarget: func() interface{} { return &strictPtrWrap{} },
			wantPath:  `Child.bogus`,
		},
		{
			name:      "renamed field",
			wire:      wireRenameWrap{Renamed: wireLeaf{Foo: "x", Bogus: "y"}},
			newTarget: func() interface{} { return &strictRenameWrap{} },
			wantPath:  `custom_name.bogus`,
		},
		{
			name:      "nested struct shallow",
			wire:      wireOuter{Title: "t", Inner: wireInner{Name: "n", Bogus: "b"}},
			newTarget: func() interface{} { return &strictOuter{} },
			wantPath:  `Inner.bogus`,
		},
		{
			name:      "nested struct deep",
			wire:      wireOuterDeep{Inner: wireInnerDeep{Name: "n", Leaf: wireLeaf{Foo: "x", Bogus: "y"}}},
			newTarget: func() interface{} { return &strictOuter{} },
			wantPath:  `Inner.Leaf.bogus`,
		},
		{
			name:      "inlined struct unknown at root level",
			wire:      wireEmbed{Embedded: "e", Bogus: "b"},
			newTarget: func() interface{} { return &strictInlineOuter{} },
			wantPath:  `Bogus`,
		},
		{
			name:      "unknown below an inlined struct keeps flattened path",
			wire:      wireEmbedDeep{Embedded: "e", Leaf: wireLeaf{Foo: "x", Bogus: "y"}},
			newTarget: func() interface{} { return &strictInlineOuter{} },
			wantPath:  `Leaf.bogus`,
		},
		{
			name:       "custom struct tag",
			wire:       wireJSONParent{Kid: wireJSONChild{Value: "v", Bad: "b"}},
			newTarget:  func() interface{} { return &strictJSONParent{} },
			wantPath:   `Kid.bad`,
			customTag:  "json",
			encodeJSON: true,
		},
		{
			name: "array-encoded struct uses logical field names",
			wire: wireArrWrap{Arr: wireAsArray{
				Name: "n",
				Leaf: wireLeaf{Foo: "x", Bogus: "y"},
			}},
			newTarget: func() interface{} { return &strictArrWrap{} },
			wantPath:  `arr.Leaf.bogus`,
		},
		{
			name: "slice element index",
			wire: wireItems{Items: []wireLeaf{
				{Foo: "a"},
				{Foo: "b", Bogus: "y"},
			}},
			newTarget: func() interface{} { return &strictItems{} },
			wantPath:  `Items[1].bogus`,
		},
		{
			name: "fixed array element index",
			wire: wireFixedItems{Items: [2]wireLeaf{
				{Foo: "a"},
				{Foo: "b", Bogus: "y"},
			}},
			newTarget: func() interface{} { return &strictFixedItems{} },
			wantPath:  `Items[1].bogus`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data []byte
			if tt.encodeJSON {
				var buf bytes.Buffer
				enc := msgpack.NewEncoder(&buf)
				enc.SetCustomStructTag(tt.customTag)
				require.NoError(t, enc.Encode(tt.wire))
				data = buf.Bytes()
			} else {
				data = mustMarshalStrict(t, tt.wire)
			}

			dec := newStrictDecoder(data)
			if tt.customTag != "" {
				dec.SetCustomStructTag(tt.customTag)
			}

			err := dec.Decode(tt.newTarget())
			require.Error(t, err)
			require.Equal(t, unknownFieldPrefix+`"`+tt.wantPath+`"`, err.Error())
			require.True(t, strings.HasPrefix(err.Error(), unknownFieldPrefix),
				"strict error must keep the recognizable prefix, got %q", err.Error())
		})
	}
}

func TestStrictErrorPrefixAtRootIsBackwardCompatible(t *testing.T) {
	data := mustMarshalStrict(t, wireLeaf{Foo: "x", Bogus: "y"})

	err := newStrictDecoder(data).Decode(&strictLeaf{})
	require.EqualError(t, err, `msgpack: unknown field "bogus"`)
}

//------------------------------------------------------------------------------
// Unknown field handler.
//------------------------------------------------------------------------------

func TestUnknownFieldHandlerSkipKeepsKnownFields(t *testing.T) {
	data := mustMarshalStrict(t, wireLeafUnknownFirst{Bogus: "bad", Foo: "good"})

	var calls []unknownFieldCall
	dec := newStrictDecoder(data)
	dec.SetUnknownFieldHandler(recordUnknownFields(&calls))

	var out strictLeaf
	require.NoError(t, dec.Decode(&out))
	require.Equal(t, "good", out.Foo)
	require.Equal(t, []unknownFieldCall{{path: "", name: "Bogus"}}, calls)
}

func TestUnknownFieldHandlerNestedPath(t *testing.T) {
	data := mustMarshalStrict(t, wireEmbed{
		Embedded: "e",
		Bogus:    "b",
		Leaf:     wireLeaf{Foo: "x", Bogus: "y"},
	})

	var calls []unknownFieldCall
	dec := newStrictDecoder(data)
	dec.SetUnknownFieldHandler(recordUnknownFields(&calls))

	var out strictInlineOuter
	require.NoError(t, dec.Decode(&out))
	require.Equal(t, "e", out.Embedded)
	require.Equal(t, "x", out.Leaf.Foo)
	require.Equal(t, []unknownFieldCall{
		{path: "", name: "Bogus"},
		{path: "Leaf", name: "bogus"},
	}, calls)
	// The path is fully unwound after decoding.
	require.Equal(t, "", dec.Path())
}

func TestUnknownFieldHandlerCustomError(t *testing.T) {
	data := mustMarshalStrict(t, wireLeaf{Foo: "x", Bogus: "y"})

	sentinel := errors.New("policy rejects unknown fields")
	dec := newStrictDecoder(data)
	dec.SetUnknownFieldHandler(func(*msgpack.Decoder, string) error {
		return sentinel
	})

	var out strictLeaf
	err := dec.Decode(&out)
	require.Error(t, err)
	require.True(t, errors.Is(err, sentinel))
}

func TestUnknownFieldHandlerWorksWithoutStrictMode(t *testing.T) {
	data := mustMarshalStrict(t, wireLeaf{Foo: "x", Bogus: "y"})

	var calls []unknownFieldCall
	dec := msgpack.NewDecoder(bytes.NewReader(data))
	dec.SetUnknownFieldHandler(recordUnknownFields(&calls))

	var out strictLeaf
	require.NoError(t, dec.Decode(&out))
	require.Equal(t, "x", out.Foo)
	require.Equal(t, []unknownFieldCall{{path: "", name: "bogus"}}, calls)
}

//------------------------------------------------------------------------------
// Reset / streaming hygiene.
//------------------------------------------------------------------------------

func TestDecoderResetClearsPathAndHandler(t *testing.T) {
	data := mustMarshalStrict(t, wireOuterDeep{
		Inner: wireInnerDeep{Leaf: wireLeaf{Foo: "x", Bogus: "y"}},
	})

	dec := newStrictDecoder(data)
	err := dec.Decode(&strictOuter{})
	require.EqualError(t, err, `msgpack: unknown field "Inner.Leaf.bogus"`)
	// Balanced push/pop leaves no path residue after a failed decode.
	require.Equal(t, "", dec.Path())

	good := mustMarshalStrict(t, wireLeafUnknownFirst{Bogus: "skip", Foo: "ok"})
	calls := 0
	dec.SetUnknownFieldHandler(func(*msgpack.Decoder, string) error {
		calls++
		return nil
	})
	// Reset clears flags, handler and any path state.
	dec.Reset(bytes.NewReader(good))
	require.Equal(t, "", dec.Path())

	var out strictLeaf
	require.NoError(t, dec.Decode(&out))
	require.Equal(t, "ok", out.Foo)
	require.Equal(t, 0, calls, "Reset must clear the unknown field handler")
}

func TestStreamedDecodeDoesNotLeakPath(t *testing.T) {
	deep := mustMarshalStrict(t, wireOuterDeep{
		Inner: wireInnerDeep{Leaf: wireLeaf{Foo: "x", Bogus: "deep"}},
	})
	root := mustMarshalStrict(t, wireLeaf{Foo: "x", Bogus: "root"})

	stream := append(append([]byte{}, deep...), root...)
	dec := msgpack.NewDecoder(bytes.NewReader(stream))
	dec.DisallowUnknownFields(true)

	// The first message is fully consumed by a skip-and-continue handler,
	// so the decoder is positioned at the start of the second message.
	var calls []unknownFieldCall
	dec.SetUnknownFieldHandler(recordUnknownFields(&calls))

	var outer strictOuter
	require.NoError(t, dec.Decode(&outer))
	require.Equal(t, []unknownFieldCall{{path: "Inner.Leaf", name: "bogus"}}, calls)
	require.Equal(t, "", dec.Path())

	// Switch to the default strict error. The second message is rooted
	// again and must not inherit the first message's path.
	dec.SetUnknownFieldHandler(nil)

	var leaf strictLeaf
	err := dec.Decode(&leaf)
	require.EqualError(t, err, `msgpack: unknown field "bogus"`,
		"second message must not inherit the path left by the first message")
}

//------------------------------------------------------------------------------
// Malformed input.
//------------------------------------------------------------------------------

func TestStrictMalformedInputHasNoUnknownFieldDiagnostic(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		wantErr string
	}{
		{
			name:    "nil key",
			raw:     []byte{0x81, 0xc0, 0xc0},
			wantErr: "msgpack: invalid code=c0 decoding struct field name",
		},
		{
			name: "integer key",
			raw:  []byte{0x81, 0x00, 0xc0},
		},
		{
			name: "truncated known value",
			raw:  []byte{0x81, 0xa3, 'f', 'o', 'o'},
		},
		{
			name: "declared map length exceeds entries",
			raw:  []byte{0x82, 0xa3, 'f', 'o', 'o', 0xa1, 'x'},
		},
		{
			name: "truncated map header",
			raw:  []byte{0x81},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []unknownFieldCall
			dec := newStrictDecoder(tt.raw)
			dec.SetUnknownFieldHandler(recordUnknownFields(&calls))

			var out strictLeaf
			err := dec.Decode(&out)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "unknown field",
				"malformed input must not be reported as an unknown field: %v", err)
			require.Empty(t, calls, "the handler must not be invoked for malformed input")
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

// A valid, genuinely unknown key followed by a truncated value is still a
// real unknown field: strict mode keeps its usual error, and when a
// handler chooses to skip the decoder must then fail while skipping the
// value instead of silently accepting the stream.
func TestUnknownKeyWithTruncatedValue(t *testing.T) {
	raw := []byte{0x81, 0xa5, 'B', 'o', 'g', 'u', 's'}

	t.Run("strict default error is unchanged", func(t *testing.T) {
		err := newStrictDecoder(raw).Decode(&strictLeaf{})
		require.EqualError(t, err, `msgpack: unknown field "Bogus"`)
	})

	t.Run("handler continue surfaces the skip failure", func(t *testing.T) {
		var calls []unknownFieldCall
		dec := newStrictDecoder(raw)
		dec.SetUnknownFieldHandler(recordUnknownFields(&calls))

		err := dec.Decode(&strictLeaf{})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "unknown field")
		require.Equal(t, []unknownFieldCall{{path: "", name: "Bogus"}}, calls)
	})
}

func TestNilKeyRejectedWithoutStrictMode(t *testing.T) {
	dec := msgpack.NewDecoder(bytes.NewReader([]byte{0x81, 0xc0, 0xc0}))

	var out strictLeaf
	err := dec.Decode(&out)
	require.EqualError(t, err, "msgpack: invalid code=c0 decoding struct field name")
}

//------------------------------------------------------------------------------
// Compatibility: non-strict mode, map targets, custom map decoders and
// existing usage without unknown fields.
//------------------------------------------------------------------------------

func TestNonStrictUnknownFieldsRemainAccepted(t *testing.T) {
	data := mustMarshalStrict(t, wireOuter{
		Title: "t",
		Inner: wireInner{Name: "n", Bogus: "b", Leaf: wireLeaf{Foo: "x", Bogus: "y"}},
	})

	dec := msgpack.NewDecoder(bytes.NewReader(data))
	var out strictOuter
	require.NoError(t, dec.Decode(&out))
	require.Equal(t, "t", out.Title)
	require.Equal(t, "n", out.Inner.Name)
	require.Equal(t, "x", out.Inner.Leaf.Foo)
}

func TestUnmarshalUnknownFieldsRemainAccepted(t *testing.T) {
	data := mustMarshalStrict(t, wireLeaf{Foo: "x", Bogus: "y"})

	var out strictLeaf
	require.NoError(t, msgpack.Unmarshal(data, &out))
	require.Equal(t, "x", out.Foo)
}

func TestStrictModeIgnoresMapTargets(t *testing.T) {
	data := mustMarshalStrict(t, map[string]interface{}{
		"foo":   "bar",
		"bogus": 1,
	})

	t.Run("map[string]interface{}", func(t *testing.T) {
		var m map[string]interface{}
		require.NoError(t, newStrictDecoder(data).Decode(&m))
		require.Equal(t, "bar", m["foo"])
		require.Equal(t, int8(1), m["bogus"])
	})

	t.Run("struct field with map value", func(t *testing.T) {
		wire := struct {
			M map[string]interface{}
		}{
			M: map[string]interface{}{"foo": "bar", "bogus": 1},
		}
		var out strictWithMap
		require.NoError(t, newStrictDecoder(mustMarshalStrict(t, wire)).Decode(&out))
		require.Equal(t, int8(1), out.M["bogus"])
	})
}

func TestStrictModeIgnoresCustomMapDecoder(t *testing.T) {
	data := mustMarshalStrict(t, map[string]interface{}{
		"foo":   "bar",
		"bogus": 1,
	})

	dec := newStrictDecoder(data)
	called := false
	dec.SetMapDecoder(func(d *msgpack.Decoder) (interface{}, error) {
		called = true
		return d.DecodeMap()
	})

	var v interface{}
	require.NoError(t, dec.Decode(&v))
	require.True(t, called)
	m := v.(map[string]interface{})
	require.Equal(t, int8(1), m["bogus"])
}

func TestStrictRoundTripsWithoutUnknownFields(t *testing.T) {
	t.Run("map encoded structs", func(t *testing.T) {
		in := &strictOuter{
			Title: "t",
			Inner: strictInner{Name: "n", Leaf: strictLeaf{Foo: "x"}},
		}
		data := mustMarshalStrict(t, in)

		var out strictOuter
		require.NoError(t, newStrictDecoder(data).Decode(&out))
		require.Equal(t, *in, out)
	})

	t.Run("inlined structs", func(t *testing.T) {
		in := &strictInlineOuter{strictEmbed{
			Embedded: "e",
			Leaf:     strictLeaf{Foo: "x"},
		}}
		data := mustMarshalStrict(t, in)

		var out strictInlineOuter
		require.NoError(t, newStrictDecoder(data).Decode(&out))
		require.Equal(t, *in, out)
	})

	t.Run("array encoded structs", func(t *testing.T) {
		in := &strictArrWrap{Arr: strictAsArray{
			Name: "n",
			Leaf: strictLeaf{Foo: "x"},
		}}
		data := mustMarshalStrict(t, in)

		var out strictArrWrap
		require.NoError(t, newStrictDecoder(data).Decode(&out))
		require.Equal(t, *in, out)
	})

	t.Run("slices and arrays of structs", func(t *testing.T) {
		inSlice := &strictItems{Items: []strictLeaf{{Foo: "a"}, {Foo: "b"}}}
		data := mustMarshalStrict(t, inSlice)

		var outSlice strictItems
		require.NoError(t, newStrictDecoder(data).Decode(&outSlice))
		require.Equal(t, *inSlice, outSlice)

		inFixed := &strictFixedItems{Items: [2]strictLeaf{{Foo: "a"}, {Foo: "b"}}}
		data = mustMarshalStrict(t, inFixed)

		var outFixed strictFixedItems
		require.NoError(t, newStrictDecoder(data).Decode(&outFixed))
		require.Equal(t, *inFixed, outFixed)
	})
}
