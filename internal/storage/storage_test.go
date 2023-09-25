package storage

import (
	"testing"

	"github.com/dev-ansh-r/loraserver/internal/test"
	"github.com/stretchr/testify/suite"
)

type StorageTestSuite struct {
	suite.Suite
	test.DatabaseTestSuiteBase
}

func TestStorage(t *testing.T) {
	suite.Run(t, new(StorageTestSuite))
}
