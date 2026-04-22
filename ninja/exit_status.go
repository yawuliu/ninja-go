package main

type ExitStatus int

const (
	ExitSuccess     ExitStatus = 0
	ExitFailure     ExitStatus = 1
	ExitInterrupted ExitStatus = 2
)
