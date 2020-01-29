package main

import (
	"fmt"

	"github.com/matope/go-change-ref/example/pkg2"
)

type T1 int

func (t *T1) Method1() {}
func (t T1) Method2()  {}

func main() {
	var t1 T1
	fmt.Println(t1)

	var t2 pkg2.T2
	fmt.Println(t2)

	var localDummy TDummy
	var t2_dummy *pkg2.Pkg2Dummy
	fmt.Println(t2_dummy)
	var t4_dummy *pkg4.Pkg3Dummy
}
