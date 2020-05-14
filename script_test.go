package tengox_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/require"
	"github.com/d5/tengo/v2/stdlib"

	"github.com/ozanh/tengox"
)

const module = `
add := func(a, b, ...c) {
	r := a + b
	for v in c {
		r += v
	}
	return r
}

mul := func(a, b, ...c) {
	r := a * b
	for v in c {
		r *= v
	}
	return r
}

square := func(a) {
	return mul(a, a)
}

fib := func(x) {
	if x == 0 {
		return 0
	} else if x == 1 {
		return 1
	}
	return fib(x-1) + fib(x-2)
}

// builtins must be wrapped like this since they are not in globals
stringer := func(s) {
	return string(s)
}
`

func TestCallByName(t *testing.T) {
	type testArgs struct {
		fn   string
		args []interface{}
		ret  interface{}
	}
	tests := []testArgs{
		{fn: "add", args: []interface{}{3, 4}, ret: int64(7)},
		{fn: "add", args: []interface{}{1, 2, 3, 4}, ret: int64(10)},
		{fn: "mul", args: []interface{}{3, 4}, ret: int64(12)},
		{fn: "mul", args: []interface{}{1, 2, 3, 4}, ret: int64(24)},
		{fn: "square", args: []interface{}{3}, ret: int64(9)},
		{fn: "fib", args: []interface{}{10}, ret: int64(55)},
		{fn: "stringer", args: []interface{}{12345}, ret: "12345"},
	}
	ctx := context.Background()
	script := tengox.NewScript([]byte(module))
	compl, err := script.CompileRun()
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		var comp *tengox.Compiled
		if i == 0 {
			// use same script for each test
			comp = compl
		} else if i == 1 {
			// create script for each test
			scr := tengox.NewScript([]byte(module))
			comp, err = scr.CompileRun()
			require.NoError(t, err)
		} else {
			// use clone
			comp = compl.Clone()
		}
		for _, test := range tests {
			result, err := comp.CallByName(test.fn, test.args...)
			require.NoError(t, err)
			require.Equal(t, test.ret, result)

			resultx, err := comp.CallByNameContext(ctx, test.fn, test.args...)
			require.NoError(t, err)
			require.Equal(t, test.ret, resultx)
		}
	}

}

func TestCallback(t *testing.T) {
	const callbackModule = `
b := 2

pass(func(a) {
	return a * b
})
`
	scr := tengox.NewScript([]byte(callbackModule))

	var callback *tengox.Callback
	scr.Add("pass", &tengo.UserFunction{
		Value: func(args ...tengo.Object) (tengo.Object, error) {
			callback = tengox.NewCallback(args[0])
			return tengo.UndefinedValue, nil
		},
	})

	compl, err := scr.CompileRun()
	require.NoError(t, err)
	require.NotNil(t, callback)
	// unset *Compiled throws error
	result, err := callback.Call(3)
	require.Error(t, err)

	// Set *Compiled before Call
	result, err = callback.Set(compl).Call(3)
	require.NoError(t, err)
	require.Equal(t, int64(6), result)

	result, err = callback.Set(compl).Call(5)
	require.NoError(t, err)
	require.Equal(t, int64(10), result)

	// Modify the global and check the new value is reflected in the function
	compl.Set("b", 3)
	result, err = callback.Set(compl).Call(5)
	require.NoError(t, err)
	require.Equal(t, int64(15), result)

	c := callback.Set(compl)
	resultx, err := c.CallContext(context.Background(), 5)
	require.Equal(t, result, resultx)
}

func TestClosure(t *testing.T) {
	const closureModule = `
mulClosure := func(a) {
	return func(b) {
		return a * b
	}
}

mul2 := mulClosure(2)
mul3 := mulClosure(3)
`

	scr := tengox.NewScript([]byte(closureModule))

	compl, err := scr.CompileRun()
	require.NoError(t, err)

	result, err := compl.CallByName("mul2", 3)
	require.NoError(t, err)
	require.Equal(t, int64(6), result)

	result, err = compl.CallByName("mul2", 5)
	require.NoError(t, err)
	require.Equal(t, int64(10), result)

	result, err = compl.CallByName("mul3", 5)
	require.NoError(t, err)
	require.Equal(t, int64(15), result)
}

func TestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	compl, err := tengox.NewScript([]byte("")).CompileRunContext(ctx)
	require.Error(t, err)
	require.Equal(t, context.Canceled.Error(), err.Error())

	ctx, cancel = context.WithTimeout(context.Background(), 0)
	defer cancel()
	scr := tengox.NewScript([]byte(module))
	compl, err = scr.CompileRunContext(context.Background())
	require.NoError(t, err)
	_, err = compl.CallByNameContext(ctx, "square", 2)
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded.Error(), err.Error())
}

func TestImportCall(t *testing.T) {
	module := `contains := import("text").contains`
	scr := tengox.NewScript([]byte(module))
	mm := stdlib.GetModuleMap(stdlib.AllModuleNames()...)
	scr.SetImports(mm)
	compl, err := scr.CompileRun()
	require.NoError(t, err)
	v, err := compl.CallByName("contains", "foo bar", "bar")
	require.NoError(t, err)
	require.Equal(t, true, v)

	v, err = compl.CallByName("contains", "foo bar", "baz")
	require.NoError(t, err)
	require.Equal(t, false, v)

	v, err = compl.CallByName("containsX", "foo bar", "bar")
	require.True(t, strings.Contains(err.Error(), "not found"))
}

func TestCallable(t *testing.T) {
	// taken from tengo quick start example
	src := `
each := func(seq, fn) {
    for x in seq { fn(x) }
}

sum := 0
mul := 1

f := func(x) {
	sum += x
	mul *= x
}

each([a, b, c, d], f)`

	script := tengox.NewScript([]byte(src))

	// set values
	err := script.Add("a", 1)
	require.NoError(t, err)
	err = script.Add("b", 9)
	require.NoError(t, err)
	err = script.Add("c", 8)
	require.NoError(t, err)
	err = script.Add("d", 4)
	require.NoError(t, err)

	// compile and run the script
	compl, err := script.CompileRunContext(context.Background())
	require.NoError(t, err)

	// retrieve values
	sum := compl.Get("sum")
	mul := compl.Get("mul")
	require.Equal(t, 22, sum.Int())
	require.Equal(t, 288, mul.Int())

	_, err = compl.CallByName("f", 2)
	require.NoError(t, err)

	sum = compl.Get("sum")
	mul = compl.Get("mul")
	require.Equal(t, 22+2, sum.Int())
	require.Equal(t, 288*2, mul.Int())

	var args []tengo.Object
	eachf := &tengoCallable{
		callFunc: func(a ...tengo.Object) (tengo.Object, error) {
			if len(a) != 1 {
				panic(fmt.Errorf("1 argument is expected but got %d",
					len(a)))
			}
			args = append(args, a[0])
			return tengo.UndefinedValue, nil
		},
	}
	nums := []interface{}{1, 2, 3, 4}
	_, err = compl.CallByName("each", nums, eachf)
	require.NoError(t, err)
	require.Equal(t, 4, len(args))
	for i, v := range args {
		vv := tengo.ToInterface(v)
		require.Equal(t, int64(nums[i].(int)), vv.(int64))
	}
}

func TestGetAll(t *testing.T) {
	script := tengox.NewScript([]byte(module))
	compl, err := script.CompileRunContext(context.Background())
	require.NoError(t, err)
	vars := compl.GetAll()
	varsMap := make(map[string]bool)
	for _, v := range vars {
		varsMap[v.Name()] = true
	}
	names := []string{"add", "mul", "square", "fib", "stringer"}
	for _, v := range names {
		require.True(t, compl.IsDefined(v))
		require.True(t, varsMap[v])
	}
}

type tengoCallable struct {
	tengo.ObjectImpl
	callFunc tengo.CallableFunc
}

func (tc *tengoCallable) CanCall() bool {
	return true
}

func (tc *tengoCallable) Call(args ...tengo.Object) (tengo.Object, error) {
	return tc.callFunc(args...)
}
