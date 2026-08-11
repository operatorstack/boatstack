### Initialize fresh clones from an exact runtime pin

Fresh clones can now use their committed runtime pin to initialize missing
machine-local controller state. Boatstack verifies that the pin matches the
executing candidate and immutable artifact before initialization; malformed,
mismatched, missing, or corrupted runtime evidence still fails closed.
