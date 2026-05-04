package main

// DepfileParserOptions 目前为空，保留用于扩展
type DepfileParserOptions struct{}

// DepfileParser 解析 depfile 内容
type DepfileParser struct {
	Outs    []string
	Ins     []string
	options *DepfileParserOptions
}

// NewDepfileParser 创建解析器实例
func NewDepfileParser(options *DepfileParserOptions) *DepfileParser {
	return &DepfileParser{
		options: options,
	}
}

func isPlainTextChar(ch byte) bool {
	if ch >= 'a' && ch <= 'z' {
		return true
	}
	if ch >= 'A' && ch <= 'Z' {
		return true
	}
	if ch >= '0' && ch <= '9' {
		return true
	}
	switch ch {
	case '+', '?', '"', '\'', '&', ',', '/', '_', ':', '.',
		'~', '(', ')', '{', '}', '%', '=', '@', '[', ']', '!', '-':
		return true
	}
	if ch >= 0x80 {
		return true
	}
	return false
}

// Parse 解析 depfile 内容。
func (p *DepfileParser) Parse(content string, err *string) bool {
	b := []byte(content)
	pos := 0
	end := len(b)
	haveTarget := false
	parsingTargets := true
	poisonedInput := false
	isEmpty := true

	for pos < end {
		haveNewline := false
		out := pos
		filename := out

		for pos < end {
			start := pos

			// Count leading backslashes
			slashCount := 0
			for pos < end && b[pos] == '\\' {
				slashCount++
				pos++
			}

			if slashCount > 0 && pos < end {
				next := b[pos]

				if next == '\n' {
					// backslash + newline = line continuation
					pos++
					break
				}
				if next == '\r' && pos+1 < end && b[pos+1] == '\n' {
					pos += 2
					break
				}

				if next == ' ' && slashCount%2 == 1 {
					// 2N+1 backslashes + space → N backslashes + space
					n := slashCount/2 - 1
					for i := 0; i < n; i++ {
						b[out] = '\\'
						out++
					}
					b[out] = ' '
					out++
					pos++ // consume space
					continue
				}

				if next == ' ' && slashCount%2 == 0 {
					// 2N backslashes + space → 2N backslashes, end of filename
					for i := 0; i < slashCount-1; i++ {
						b[out] = '\\'
						out++
					}
					// don't consume space - filename terminator
					break
				}

				if next == '#' {
					// backslash(s) + # → de-escape hash
					for i := 0; i < slashCount-1; i++ {
						b[out] = '\\'
						out++
					}
					b[out] = '#'
					out++
					pos++ // consume #
					continue
				}

				if next == ':' && pos+1 < end {
					after := b[pos+1]
					if after == ' ' || after == '\t' || after == '\r' || after == '\n' {
						// backslash(s) + : + whitespace → plain text
						pos++ // consume :
						for i := 0; i < slashCount; i++ {
							b[out] = '\\'
							out++
						}
						b[out] = ':'
						out++
						if b[pos-1] == '\n' {
							haveNewline = true
						}
						break
					}
				}

				if next == ':' {
					// backslash(s) + : (not followed by whitespace) → de-escape colon
					for i := 0; i < slashCount-1; i++ {
						b[out] = '\\'
						out++
					}
					b[out] = ':'
					out++
					pos++ // consume :
					continue
				}

				// backslash + other char: treat as plain text
				// Write all backslashes
				for i := 0; i < slashCount; i++ {
					b[out] = '\\'
					out++
				}
				pos = start + slashCount // reset to after backslashes
				for pos < end && isPlainTextChar(b[pos]) {
					b[out] = b[pos]
					out++
					pos++
				}
				continue
			}

			if pos >= end {
				// Put back any backslashes we consumed
				for i := 0; i < slashCount; i++ {
					b[out] = '\\'
					out++
				}
				break
			}

			// Not a backslash sequence
			pos = start // reset to original position
			ch := b[pos]

			// '$'
			if ch == '$' {
				if pos+1 < end && b[pos+1] == '$' {
					b[out] = '$'
					out++
					pos += 2
					continue
				}
			}

			// newline
			if ch == '\n' {
				haveNewline = true
				pos++
				break
			}
			if ch == '\r' && pos+1 < end && b[pos+1] == '\n' {
				haveNewline = true
				pos += 2
				break
			}

			// plain text
			if isPlainTextChar(ch) {
				for pos < end && isPlainTextChar(b[pos]) {
					b[out] = b[pos]
					out++
					pos++
				}
				continue
			}

			// Any other character (e.g. whitespace, colon separator)
			pos++
			break
		}

		// Process accumulated filename
		length := out - filename
		isDependency := !parsingTargets
		if length > 0 && b[filename+length-1] == ':' {
			length--
			parsingTargets = false
			haveTarget = true
		}

		if length > 0 {
			isEmpty = false
			piece := string(b[filename : filename+length])
			found := false
			for _, in := range p.Ins {
				if in == piece {
					found = true
					break
				}
			}
			if !found {
				if isDependency {
					if poisonedInput {
						*err = "inputs may not also have inputs"
						return false
					}
					p.Ins = append(p.Ins, piece)
				} else {
					foundOut := false
					for _, out := range p.Outs {
						if out == piece {
							foundOut = true
							break
						}
					}
					if !foundOut {
						p.Outs = append(p.Outs, piece)
					}
				}
			} else if !isDependency {
				poisonedInput = true
			}
		}

		if haveNewline {
			parsingTargets = true
			poisonedInput = false
		}
	}

	if !haveTarget && !isEmpty {
		*err = "expected ':' in depfile"
		return false
	}
	return true
}

// DepfileError 自定义错误类型
type DepfileError struct {
	Msg string
}

func (e *DepfileError) Error() string {
	return e.Msg
}
