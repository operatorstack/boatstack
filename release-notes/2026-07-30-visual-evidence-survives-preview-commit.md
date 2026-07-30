### Visual evidence stays trusted when you commit the reviewed pr.md

Boatstack now trusts recorded visual evidence by product identity: the manifest stays `PASS` while the product diff is unchanged. Before this change, the mandatory commit of the reviewed `pr.md` moved the head commit and always degraded `PASS` evidence to `NOT_VERIFIED`, so `require` could not reach publication with current screenshots. Any change to product content still makes the evidence stale immediately. The capture commit stays recorded and the PR preview names it in the Visual evidence table.
