package px

import lua "github.com/Shopify/go-lua"

func noArgs(st *lua.State, fname string) bool {
	return assertArgsN(st, fname, 0)
}

func assertArgsN(st *lua.State, fname string, exp int) bool {
	n := st.Top()
	if n == exp {
		return true
	}
	lua.Errorf(st, "bad argument count to '%s' (%d expected, got %d)", fname, exp, n)
	st.SetTop(0)
	return false
}

func assertArgsT(st *lua.State, fname string, types ...lua.Type) bool {
	if !assertArgsN(st, fname, len(types)) {
		return false
	}
	for i, t := range types {
		ind := i + 1
		if st.TypeOf(ind) != t {
			lua.CheckType(st, ind, t)
			st.SetTop(0)
			return false
		}
	}
	return true
}
