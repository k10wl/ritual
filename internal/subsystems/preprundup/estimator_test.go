package preprundup_test

import (
	"ritual/internal/subsystems/preprundup"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeLoader struct {
	file preprundup.File
	err  error
}

func (f fakeLoader) Load() (preprundup.File, error) { return f.file, f.err }

func TestEstimator_NoHistory_ReturnsZero(t *testing.T) {
	e := preprundup.NewEstimator(fakeLoader{file: preprundup.File{}})
	assert.Equal(t, time.Duration(0), e.PrepEta())
	assert.Equal(t, time.Duration(0), e.WrapEta())
}

func TestEstimator_ReturnsLastSampleDirectly_NoAveraging(t *testing.T) {
	e := preprundup.NewEstimator(fakeLoader{file: preprundup.File{
		Last: &preprundup.Sample{PrepMs: 14200, WrapMs: 28800},
	}})
	assert.Equal(t, 14200*time.Millisecond, e.PrepEta())
	assert.Equal(t, 28800*time.Millisecond, e.WrapEta())
}

func TestEstimator_LoadError_ReturnsZero(t *testing.T) {
	e := preprundup.NewEstimator(fakeLoader{err: assertErr})
	assert.Equal(t, time.Duration(0), e.PrepEta())
}

var assertErr = &loadErr{}

type loadErr struct{}

func (*loadErr) Error() string { return "boom" }
