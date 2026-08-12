package effects

import "sync"

type lockedMutex struct{ sync.Mutex }
