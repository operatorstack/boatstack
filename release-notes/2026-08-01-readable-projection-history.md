### Make projection history readable

Generated sync pull requests and commits now use the latest relevant source
commit subject instead of only showing an opaque source hash. Their bodies list
every included Boatstack source commit with GitHub links and retain the full
source repository and commit as provenance trailers.
