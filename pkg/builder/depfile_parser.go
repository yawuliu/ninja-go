package builder

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

// Parse 解析 depfile 内容，返回错误（如果有）
func (p *DepfileParser) Parse(content string, err *string) bool {
	// in: current parser input point.
	// end: end of input.
	// parsing_targets: whether we are parsing targets or dependencies.
	b := []byte(content)
	in := 0
	end := len(b)
	haveTarget := false
	parsingTargets := true
	poisonedInput := false
	isEmpty := true
	length := 0
	n := 0
	for in < end {
		haveNewline := false
		// out: current output point (typically same as in, but can fall behind as we de-escape backslashes).
		out := in
		// filename: start of the current parsed filename.
		filename := out

		// Re2c generated scanner (translated to Go using goto)
		// yybm table
		yybm := [256]byte{
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 128, 128, 0, 0, 128, 128, 128,
			128, 128, 0, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 0, 0, 128, 0, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 0, 128, 0, 128,
			0, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 0, 128, 128, 0,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
			128, 128, 128, 128, 128, 128, 128, 128,
		}

		var yych byte
		var yymarker int
		start := in
		yych = b[in]
		if (yybm[yych] & 128) != 0 {
			goto yy5
		}
		if yych <= '\r' {
			if yych <= '\t' {
				if yych >= 0x01 {
					goto yy1
				}
			} else {
				if yych <= '\n' {
					goto yy3
				}
				if yych <= '\f' {
					goto yy1
				}
				goto yy4
			}
		} else {
			if yych <= '$' {
				if yych <= '#' {
					goto yy1
				}
				goto yy7
			} else {
				if yych <= '>' {
					goto yy1
				}
				if yych <= '\\' {
					goto yy8
				}
				goto yy1
			}
		}
		in++
		goto yy2

	yy1:
		in++
	yy2:
		// For any other character (e.g. whitespace), swallow it here,
		// allowing the outer logic to loop around again.
		break

	yy3:
		in++
		// A newline ends the current log_file_ name and the current rule.
		haveNewline = true
		break

	yy4:
		yych = b[in+1]
		if yych == '\n' {
			goto yy3
		}
		goto yy2

	yy5:
		yych = b[in+1]
		if (yybm[yych] & 128) != 0 {
			in++
			goto yy5
		}
	yy6:
		// Got a span of plain text.
		length = in - start
		// Need to shift it over if we're overwriting backslashes.
		if out < start {
			copy(b[out:out+length], b[start:start+length])
		}
		out += length
		continue

	yy7:
		yych = b[in+1]
		if yych == '$' {
			in++
			goto yy9
		}
		goto yy2

	yy8:
		yymarker = in
		yych = b[in+1]
		if yych <= ' ' {
			if yych <= '\n' {
				if yych <= 0x00 {
					goto yy2
				}
				if yych <= '\t' {
					goto yy10
				}
				goto yy11
			} else {
				if yych == '\r' {
					goto yy12
				}
				if yych <= 0x1F {
					goto yy10
				}
				goto yy13
			}
		} else {
			if yych <= '9' {
				if yych == '#' {
					goto yy14
				}
				goto yy10
			} else {
				if yych <= ':' {
					goto yy15
				}
				if yych == '\\' {
					goto yy17
				}
				goto yy10
			}
		}
	yy9:
		in++
		// De-escape dollar character.
		b[out] = '$'
		out++
		continue

	yy10:
		in++
		goto yy6

	yy11:
		in++
		// A line continuation ends the current log_file_ name.
		break

	yy12:
		yych = b[in+2]
		if yych == '\n' {
			in += 2
			goto yy11
		}
		in = yymarker
		goto yy2

	yy13:
		in++
		// 2N+1 backslashes plus space -> N backslashes plus space.
		length = in - start
		n = length/2 - 1
		if out < start {
			for i := 0; i < n; i++ {
				b[out+i] = '\\'
			}
		}
		out += n
		b[out] = ' '
		out++
		continue

	yy14:
		in++
		// De-escape hash sign, but preserve other leading backslashes.
		length = in - start
		if length > 2 && out < start {
			for i := 0; i < length-2; i++ {
				b[out+i] = '\\'
			}
		}
		out += length - 2
		b[out] = '#'
		out++
		continue

	yy15:
		yych = b[in+1]
		if yych <= '\f' {
			if yych <= 0x00 {
				goto yy18
			}
			if yych <= 0x08 {
				goto yy16
			}
			if yych <= '\n' {
				goto yy18
			}
		} else {
			if yych <= '\r' {
				goto yy18
			}
			if yych == ' ' {
				goto yy18
			}
		}
	yy16:
		// De-escape colon sign, but preserve other leading backslashes.
		length = in - start
		if length > 2 && out < start {
			for i := 0; i < length-2; i++ {
				b[out+i] = '\\'
			}
		}
		out += length - 2
		b[out] = ':'
		out++
		continue

	yy17:
		yych = b[in+1]
		if yych <= ' ' {
			if yych <= '\n' {
				if yych <= 0x00 {
					goto yy6
				}
				if yych <= '\t' {
					goto yy10
				}
				goto yy6
			} else {
				if yych == '\r' {
					goto yy6
				}
				if yych <= 0x1F {
					goto yy10
				}
				goto yy19
			}
		} else {
			if yych <= '9' {
				if yych == '#' {
					goto yy14
				}
				goto yy10
			} else {
				if yych <= ':' {
					goto yy15
				}
				if yych == '\\' {
					goto yy20
				}
				goto yy10
			}
		}
	yy18:
		in++
		// Backslash followed by : and whitespace.
		// It is therefore normal text and not an escaped colon
		length = in - start - 1
		if out < start {
			copy(b[out:out+length], b[start:start+length])
		}
		out += length
		if b[in-1] == '\n' {
			haveNewline = true
		}
		continue

	yy19:
		in++
		// 2N backslashes plus space -> 2N backslashes, end of filename.
		length = in - start
		if out < start {
			for i := 0; i < length-1; i++ {
				b[out+i] = '\\'
			}
		}
		out += length - 1
		continue

	yy20:
		yych = b[in+1]
		if yych <= ' ' {
			if yych <= '\n' {
				if yych <= 0x00 {
					goto yy6
				}
				if yych <= '\t' {
					goto yy10
				}
				goto yy6
			} else {
				if yych == '\r' {
					goto yy6
				}
				if yych <= 0x1F {
					goto yy10
				}
				goto yy13
			}
		} else {
			if yych <= '9' {
				if yych == '#' {
					goto yy14
				}
				goto yy10
			} else {
				if yych <= ':' {
					goto yy15
				}
				if yych == '\\' {
					goto yy17
				}
				goto yy10
			}
		}
		// end of re2c

		len := out - filename
		isDependency := !parsingTargets
		if len > 0 && b[filename+len-1] == ':' {
			len-- // Strip off trailing colon, if any.
			parsingTargets = false
			haveTarget = true
		}

		if len > 0 {
			isEmpty = false
			piece := string(b[filename : filename+len])
			// If we've seen this as an input before, skip it.
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
					// New input.
					p.Ins = append(p.Ins, piece)
				} else {
					// Check for a new output.
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
				// We've passed an input on the left side; reject new inputs.
				poisonedInput = true
			}
		}

		if haveNewline {
			// A newline ends a rule so the next filename will be a new target.
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
