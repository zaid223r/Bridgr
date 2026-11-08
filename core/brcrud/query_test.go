package brcrud

import (
	"net/url"
	"testing"
)

func TestPageLinks(t *testing.T) {
	t.Parallel()
	u, _ := url.Parse("/items?title=bolt&sort=-qty")
	tests := []struct {
		name          string
		limit, offset int
		total         int64
		want          string
	}{
		{
			"empty", 10, 0, 0,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=0&sort=-qty&title=bolt>; rel="last"`,
		},
		{
			"single page", 10, 0, 7,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=0&sort=-qty&title=bolt>; rel="last"`,
		},
		{
			"first of three", 10, 0, 25,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=10&sort=-qty&title=bolt>; rel="next", </items?limit=10&offset=20&sort=-qty&title=bolt>; rel="last"`,
		},
		{
			"middle", 10, 10, 25,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=0&sort=-qty&title=bolt>; rel="prev", </items?limit=10&offset=20&sort=-qty&title=bolt>; rel="next", </items?limit=10&offset=20&sort=-qty&title=bolt>; rel="last"`,
		},
		{
			"exact multiple", 10, 10, 20,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=0&sort=-qty&title=bolt>; rel="prev", </items?limit=10&offset=10&sort=-qty&title=bolt>; rel="last"`,
		},
		{
			"unaligned offset", 10, 5, 25,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=0&sort=-qty&title=bolt>; rel="prev", </items?limit=10&offset=15&sort=-qty&title=bolt>; rel="next", </items?limit=10&offset=20&sort=-qty&title=bolt>; rel="last"`,
		},
		{
			"past the end", 10, 40, 25,
			`</items?limit=10&offset=0&sort=-qty&title=bolt>; rel="first", </items?limit=10&offset=30&sort=-qty&title=bolt>; rel="prev", </items?limit=10&offset=20&sort=-qty&title=bolt>; rel="last"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pageLinks(u, tt.limit, tt.offset, tt.total); got != tt.want {
				t.Errorf("got\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}
