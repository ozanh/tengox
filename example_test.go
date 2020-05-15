package tengox_test

import (
	"context"
	"fmt"

	"github.com/ozanh/tengox"
)

func ExampleCompiled_CallByName() {
	module := `add := func(a, b, c) { return a + b + c; }`
	script := tengox.NewScript([]byte(module))
	// script is compiled and run
	compl, err := script.CompileRun() // CompileRunContext() is available
	if err != nil {
		panic(err)
	}
	// CallByNameContext() is available
	v, err := compl.CallByName("add", 1, 2, 3) // 1+2+3
	if err != nil {
		panic(err)
	}
	fmt.Println(v)
	//Output: 6

	// you can clone your compiled code like this
	// clone := compl.Clone()
	// cloned code run in a different VM
}

func ExampleCompiled_CallByNameContext() {
	module := `stringer := func(s) { return string(s); }`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	script := tengox.NewScript([]byte(module))
	// script is compiled and run
	compl, err := script.CompileRunContext(ctx)
	if err != nil {
		panic(err)
	}
	// string function is a builtin so it is not in global variables, stringer
	// is compiled function which calls builtin string function.
	v, err := compl.CallByNameContext(ctx, "stringer", 123456)
	if err != nil {
		panic(err)
	}
	fmt.Println(v)
	//Output: 123456
}
