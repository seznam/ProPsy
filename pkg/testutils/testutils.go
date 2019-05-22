package testutils

import "github.com/sirupsen/logrus"

func FailAinsteadofB(item string, A, B interface{}) {
	logrus.Fatalf("Wrong %s, got %+v instead of %+v", item, A, B)
}

func AssertString(a, b string) {
	if a != b {
		FailAinsteadofB("string", a, b)
	}
}

func AssertInt64(a, b int64) {
	if a != b {
		FailAinsteadofB("int64", a, b)
	}
}

func AssertInt(a, b int) {
	if a != b {
		FailAinsteadofB("int", a, b)
	}
}
