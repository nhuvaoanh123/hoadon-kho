package main

import "golang.org/x/sys/windows"

// setupConsole switches the console to UTF-8 so Vietnamese text prints correctly.
func setupConsole() { _ = windows.SetConsoleOutputCP(65001) }
