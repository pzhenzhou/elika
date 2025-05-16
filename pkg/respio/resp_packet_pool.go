package respio

import "sync"

// respPacketPool is a sync.Pool for RespPacket structs.
var respPacketPool = sync.Pool{
	New: func() interface{} {
		return &RespPacket{
			Array: make([]*RespPacket, 0, 5), // Default capacity for 5 elements
		}
	},
}

// AcquireRespPacket gets a 'clean' RespPacket from the pool.
// The caller is responsible for setting the Type, Data, and populating Array as needed.
func AcquireRespPacket() *RespPacket {
	return respPacketPool.Get().(*RespPacket)
}

// ReleaseRespPacket resets a RespPacket and returns it (and its children) to the pool.
// The caller must ensure that the packet (and its Data/Array elements if they point to
// other pooled or shared resources) is no longer in use elsewhere.
func ReleaseRespPacket(p *RespPacket) {
	if p == nil {
		return
	}

	// 1. Reset basic fields to their zero/default values.
	p.Type = 0 // Or a specific "invalid" type marker if you prefer

	// 2. Handle p.Data:
	//    - If p.Data was assigned an "owned" slice (e.g., from a make([]byte) call like
	//      the 'finalData' in BulkReadStringWithMarker), setting it to nil allows the
	//      Go runtime to GC that underlying byte array when it's no longer referenced.
	//    - If p.Data pointed to a "borrowed" buffer from another pool (e.g., common.GetBufferFromPool),
	//      that borrowed buffer *must* have already been returned to its respective pool
	//      before ReleaseRespPacket is called for this RespPacket.
	//      This RespPacket struct itself does not manage the lifecycle of external []byte pools.
	p.Data = nil

	// 3. Handle p.Array (recursive release of child RespPacket objects):
	//    The RespPacket objects *within* the Array must be released back to the respPacketPool.
	for i, item := range p.Array {
		if item != nil {
			ReleaseRespPacket(item) // Recursive call
			p.Array[i] = nil        // Nil out the pointer in the slice to help GC and avoid dangling references
		}
	}
	// Reset the p.Array slice header to length 0. This keeps the underlying array (and its capacity)
	// associated with this specific *RespPacket instance, allowing it to be reused when this
	// instance is next acquired from the pool.
	p.Array = p.Array[:0]

	// 4. Return the cleaned RespPacket struct itself to the pool.
	respPacketPool.Put(p)
}
