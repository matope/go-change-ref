package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mapPresenter map[string]string

func (p mapPresenter) resultPresenter(fname, content string) error {
	if _, ok := p[fname]; ok {
		return errors.New("filename duplication occured")
	}
	p[fname] = content
	return nil
}

// absPath returns an absolute path.
func absPath(rel string) string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Join(wd, "example/pkg1/main.go")
}

var cases = []struct {
	testname string
	param    parameters
	want     mapPresenter
}{
	{
		testname: "pkg1.T1 -> pkg2.T2 at pkg1.(local->remote)",
		param: parameters{
			fromPkgPath: "github.com/matope/go-change-ref/example/pkg1",
			fromName:    "T1",
			toPkgPath:   "github.com/matope/go-change-ref/example/pkg2",
			toName:      "T2",
			dir:         "./example/pkg1",
		},
		want: mapPresenter{
			absPath("example/pkg1/main.go"): `package main

import (
	"fmt"

	"github.com/matope/go-change-ref/example/pkg2"
)

type T1 int

func (t *T1) Method1() {}
func (t T1) Method2()  {}

func main() {
	var t1 pkg2.T2
	fmt.Println(t1)

	var t2 pkg2.T2
	fmt.Println(t2)
}
`},
	},
	{
		testname: "pkg2.T2 -> pkg1.T1 at pkg1.(remote-local)",
		param: parameters{
			fromPkgPath: "github.com/matope/go-change-ref/example/pkg2",
			fromName:    "T2",
			toPkgPath:   "github.com/matope/go-change-ref/example/pkg1",
			toName:      "T1",
			dir:         "./example/pkg1",
		},
		want: mapPresenter{
			absPath("example/pkg1/main.go"): `package main

import (
	"fmt"
)

type T1 int

func (t *T1) Method1() {}
func (t T1) Method2()  {}

func main() {
	var t1 T1
	fmt.Println(t1)

	var t2 T1
	fmt.Println(t2)
}
`},
	},
	{
		testname: "pkg2.T2 -> pkg3.T3 at pkg1.(local->remote)",
		param: parameters{
			fromPkgPath: "github.com/matope/go-change-ref/example/pkg2",
			fromName:    "T2",
			toPkgPath:   "github.com/matope/go-change-ref/example/pkg3",
			toName:      "T3",
			dir:         "./example/pkg1",
		},
		want: mapPresenter{
			absPath("example/pkg1/main.go"): `package main

import (
	"fmt"

	"github.com/matope/go-change-ref/example/pkg3"
)

type T1 int

func (t *T1) Method1() {}
func (t T1) Method2()  {}

func main() {
	var t1 T1
	fmt.Println(t1)

	var t2 pkg3.T3
	fmt.Println(t2)
}
`},
	},
}

func TestProcess(t *testing.T) {
	for _, c := range cases {
		t.Run(c.testname, func(t *testing.T) {
			param := c.param
			presenter := mapPresenter{}
			param.resultPresenter = presenter.resultPresenter
			if err := process(&param); err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, c.want, presenter)
		})
	}
}
