// Package logging defines the leveled logger shared by the router, the log
// queue and the service.
package logging

import "log"

// Logger is a leveled logger. The service adapts the kardianos/service
// logger to it, so messages reach the system log at the right level.
type Logger interface {
	Infof(format string, v ...interface{})
	Warningf(format string, v ...interface{})
	Errorf(format string, v ...interface{})
}

// Std writes to the standard library logger, with a level prefix for
// warnings and errors.
type Std struct{}

func (Std) Infof(format string, v ...interface{}) {
	log.Printf(format, v...)
}

func (Std) Warningf(format string, v ...interface{}) {
	log.Printf("WARNING: "+format, v...)
}

func (Std) Errorf(format string, v ...interface{}) {
	log.Printf("ERROR: "+format, v...)
}

// OrStd returns l, or Std when l is nil.
func OrStd(l Logger) Logger {
	if l == nil {
		return Std{}
	}
	return l
}
