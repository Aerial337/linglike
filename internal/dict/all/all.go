// Package all imports every dictionary format so that dict.Open can use them.
package all

import (
	_ "github.com/aerial337/linglike/internal/dict/ld2"
	_ "github.com/aerial337/linglike/internal/dict/mdx"
	_ "github.com/aerial337/linglike/internal/dict/stardict"
	_ "github.com/aerial337/linglike/internal/dict/textdict"
)
