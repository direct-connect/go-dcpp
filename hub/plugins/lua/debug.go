package lua

import (
	"bytes"
	"fmt"
	"log"
	"strconv"

	lua "github.com/Shopify/go-lua"
)

func DebugValue(st *lua.State, at int) {
	s := st.Top()
	buf := bytes.NewBuffer(nil)
	printValue(buf, st, at)
	st.SetTop(s)
	log.Println(buf.String())
}

func printValue(buf *bytes.Buffer, st *lua.State, at int) {
	switch st.TypeOf(at) {
	case lua.TypeNone:
		buf.WriteString("none")
	case lua.TypeNil:
		buf.WriteString("nil")
	case lua.TypeBoolean:
		v := st.ToBoolean(at)
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case lua.TypeFunction:
		buf.WriteString("(func)")
	case lua.TypeUserData:
		buf.WriteString("(userdata)")
	case lua.TypeLightUserData:
		buf.WriteString("(lightuserdata)")
	case lua.TypeNumber:
		v, _ := st.ToNumber(at)
		if v == float64(int(v)) {
			fmt.Fprint(buf, int(v))
		} else {
			fmt.Fprint(buf, v)
		}
	case lua.TypeString:
		s, _ := st.ToString(at)
		buf.WriteString(strconv.Quote(s))
	case lua.TypeTable:
		buf.WriteString("{")
		defer buf.WriteString("}")
		st.PushNil()
		for i := 0; st.Next(at); i++ {
			if i != 0 {
				buf.WriteString(", ")
			}
			key, _ := st.ToString(-2)
			buf.WriteString(strconv.Quote(key))
			buf.WriteString(": ")
			printValue(buf, st, st.Top())
			st.Pop(1)
		}
	default:
		buf.WriteString("(" + lua.TypeNameOf(st, at) + ")")
	}
}
