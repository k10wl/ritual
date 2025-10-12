package mocks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMockCommandExecutor_Execute(t *testing.T) {
	mockExecutor := NewMockCommandExecutor()

	mockExecutor.On("Execute", "test", []string{"arg1", "arg2"}, "/path").Return(nil)

	err := mockExecutor.Execute("test", []string{"arg1", "arg2"}, "/path")

	assert.NoError(t, err)
	mockExecutor.AssertExpectations(t)
}

func TestMockCommandExecutor_Execute_Error(t *testing.T) {
	mockExecutor := NewMockCommandExecutor()
	expectedError := assert.AnError

	mockExecutor.On("Execute", "fail", []string{}, "/path").Return(expectedError)

	err := mockExecutor.Execute("fail", []string{}, "/path")

	assert.Error(t, err)
	mockExecutor.AssertExpectations(t)
}
