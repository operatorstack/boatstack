### Bind declarative effects to admitted control law

Product-state effects now require product mutation authority based on their
owned facets. Declarative assignments must satisfy every target condition for
the affected facet, and recovery reconstructs its write boundary from the
admitted transition instead of mutable journal data.

The declarative state-effect change keeps the existing journal schema, so an
installation update can resume its pending transaction after runtime
activation.
