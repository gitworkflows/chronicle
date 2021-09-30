package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoteUrl(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		expects string
	}{
		{
			name:    "go case",
			path:    "test-fixtures/remote-repo",
			expects: "git@github.com:wagoodman/count-goober.git",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := RemoteUrl(test.path)
			assert.NoError(t, err)
			assert.Equal(t, test.expects, actual)
		})
	}
}
