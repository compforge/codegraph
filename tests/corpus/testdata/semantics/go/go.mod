module example.org/root

go 1.26.0

require (
 example.org/root/child v0.0.0
 example.org/peer v0.0.0
)
replace example.org/root/child => ./nested
replace example.org/peer => ./peer
