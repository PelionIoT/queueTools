package queueTools

import (
	"testing"
	"fmt"
	"strconv"
	"sync"
	"time"
	"sync/atomic"

)

type testStruct struct {
	val string
}


//   1 2 3 4 5 6 7 8 9 10    11
//  [][][][][][][][][][]    []
//    
//   1 2 3 4 5
//   [][][][][]
//  
//   4 5 6 7 8 9 10 12 13 
//  [][][][][][][][][][]
//  
//  
//  

//  begin generic
// m4_define({{*NODE*}},{{*testStruct*}})  m4_define({{*FIFO*}},{{*testStructFifo*}})  
// Thread safe queue for LogBuffer
type FIFO struct {
	nextUidOut uint32 // the 'uid' of the next
	nextUid uint32
	q []*NODE	
	mutex *sync.Mutex
	condWait *sync.Cond
	condFull *sync.Cond
	maxSize uint32
	drops int
	shutdown bool
	wakeupIter int  // this is to deal with the fact that go developers
	                // decided not to implement pthread_cond_timedwait()
	                // So we use this as a work around to temporarily wakeup
	                // (but not shutdown) the queue. Bring your own timer.
}
func GENERIC_New(FIFO)(maxsize uint32) (ret *FIFO) {
	ret = new(FIFO)
	ret.mutex =  new(sync.Mutex)
	ret.condWait = sync.NewCond(ret.mutex)
	ret.condFull = sync.NewCond(ret.mutex)
	ret.maxSize = maxsize
	ret.drops = 0
	ret.shutdown = false
	ret.wakeupIter = 0
	ret.nextUid = 0
	ret.nextUidOut = 0
	return
}
func (fifo *FIFO) Push(n *NODE) (drop bool, dropped *NODE) {
	drop = false
	DEBUG_OUT(" >>>>>>>>>>>> In Push\n")
	fifo.mutex.Lock()
	DEBUG_OUT(" ------------ In Push (past Lock)\n")
    if int(fifo.maxSize) > 0 && len(fifo.q)+1 > int(fifo.maxSize) {
    	// drop off the queue
    	dropped = (fifo.q)[0]
    	fifo.q = (fifo.q)[1:]
    	fifo.drops++
    	fifo.nextUidOut++
    	DEBUG_OUT("!!! Dropping NODE in FIFO \n")
    	drop = true
    }
    fifo.q = append(fifo.q, n)
    fifo.nextUid++
	DEBUG_OUT(" ------------ In Push (@ Unlock)\n")
    fifo.mutex.Unlock()
    fifo.condWait.Signal()
	DEBUG_OUT(" <<<<<<<<<<< Return Push\n")
    return
}
func (fifo *FIFO) PushBatch(n []*NODE) (drop bool, dropped []*NODE) {
	drop = false
	DEBUG_OUT(" >>>>>>>>>>>> In PushBatch\n")
	fifo.mutex.Lock()
	DEBUG_OUT(" ------------ In PushBatch (past Lock)\n")
	_len := uint32(len(fifo.q))
	_inlen := uint32(len(n))
	if fifo.maxSize > 0 && _inlen > fifo.maxSize {
		_inlen = fifo.maxSize
	}
    if fifo.maxSize > 0 && _len+_inlen > fifo.maxSize {
    	needdrop := _inlen+_len - fifo.maxSize 
    	if needdrop >= fifo.maxSize {
	    	drop = true
    		dropped = fifo.q
	    	fifo.q = nil
	    	fifo.nextUidOut += fifo.maxSize
    	} else if needdrop > 0 {
	    	drop = true
	    	dropped = (fifo.q)[0:needdrop]
	    	fifo.q=(fifo.q)[needdrop:]
	    	fifo.nextUidOut += needdrop
	    }
    	// // drop off the queue
    	// dropped = (fifo.q)[0]
    	// fifo.q = (fifo.q)[1:]
    	// fifo.drops++
    	DEBUG_OUT(" ----------- PushBatch() !!! Dropping %d NODE in FIFO \n", len(dropped))
    }
    DEBUG_OUT(" ----------- In PushBatch (pushed %d)\n",_inlen)
    fifo.q = append(fifo.q, n[0:int(_inlen)]...)
    fifo.nextUid += _inlen
	DEBUG_OUT(" ------------ In PushBatch (@ Unlock)\n")
    fifo.mutex.Unlock()
    fifo.condWait.Signal()
	DEBUG_OUT(" <<<<<<<<<<< Return PushBatch\n")
    return
}


func (fifo *FIFO) Pop() (n *NODE) {
	fifo.mutex.Lock()
	if len(fifo.q) > 0 {
	    n = (fifo.q)[0]
	    fifo.q = (fifo.q)[1:]
	    fifo.nextUidOut++
		fifo.condFull.Signal()
	} 
	fifo.mutex.Unlock()
    return
}
// func (fifo *FIFO) PopBatch(max uint32) (n *NODE) {
// 	fifo.mutex.Lock()
// 	_len := len(fifo.q)
// 	if _len > 0 {
// 		if _len >= max {

// 		} else {

// 		}
// 	    n = (fifo.q)[0]
// 	    fifo.q = (fifo.q)[1:]		
// 		fifo.condFull.Signal()
// 	} 
// 	fifo.mutex.Unlock()
//     return
// }
func (fifo *FIFO) PopBatch(max uint32) (slice []*NODE) {
	DEBUG_OUT(" >>>>>>>>>>>> In PopOrWaitBatch (Lock)\n")
	fifo.mutex.Lock()
//	_wakeupIter := fifo.wakeupIter
	if(fifo.shutdown) {
		fifo.mutex.Unlock()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 1)\n")
		return
	}
	_len := uint32(len(fifo.q))
	if _len > 0 {
		if  max >= _len {
	    	slice = fifo.q
	    	fifo.nextUidOut+= _len
	    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
		} else {
			fifo.nextUidOut += max
			slice = (fifo.q)[0:max]
			fifo.q = (fifo.q)[max:]
		}
		fifo.mutex.Unlock()
		fifo.condFull.Signal()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 2)\n")
		return
	}
	fifo.mutex.Unlock()
	return
}

// Works by copying a batch of queued values to a slice.
// If the caller decided it really want to remove that which 
// it earlier Peek(ed) then it will use the 'uid' value to tell
// the FIFO which Peek it was. If that batch is not already gone, it will 
// then be removed. 
func (fifo *FIFO) PeekBatch(max uint32) (slice []*NODE, uid uint32) {
	DEBUG_OUT(" >>>>>>>>>>>> In PeekBatch (Lock)\n")
	fifo.mutex.Lock()
//	_wakeupIter := fifo.wakeupIter
	if(fifo.shutdown) {
		fifo.mutex.Unlock()
		DEBUG_OUT(" <<<<<<<<<<<<< In PeekBatch (Unlock 1 - shutdown)\n")
		return
	}
	_len := uint32(len(fifo.q))
		DEBUG_OUT(" <<<<<<<<<<<<< In PeekBatch %d\n",_len)
	if _len > 0 {
		uid = fifo.nextUidOut
		DEBUG_OUT(" <<<<<<<<<<<<< In PeekBatch uid %d\n",uid)
		// we make a copy of the slice, so that all elements will be available, regardless if 
		// the FIFO itself bumps these off the queue later
		if  max >= _len {
			slice = make([]*NODE,_len)
			copy(slice,fifo.q)
	    	slice = fifo.q
//	    	fifo.nextUidOut+= _len
//	    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
		} else {
//			fifo.nextUidOut += max
			slice = make([]*NODE,max)
			copy(slice,fifo.q)
//			slice = (fifo.q)[0:max]
//			fifo.q = (fifo.q)[max:]
		}
		fifo.mutex.Unlock()
//		fifo.condFull.Signal()
		DEBUG_OUT(" <<<<<<<<<<<<< In PeekBatch (Unlock 2)\n")
		return
	}
	fifo.mutex.Unlock()
	return
}


// Removes the stated 'peek', which is the slice of nodes
// provided by the PeekBatch function, and the 'uid' that
// was returned by that function
func (fifo *FIFO) RemovePeeked(slice []*NODE, uid uint32) {
	// if for some reason the nextUidOut is less than 
	// the uid provided, it means the uid value was high it flowed over
	// and just ignore this call
	fifo.mutex.Lock()
	if slice != nil && uid <= fifo.nextUidOut {
		_len := uint32(len(slice))
		if _len > 0 {
			_offset := (fifo.nextUidOut - uid)
			_fifolen := uint32(len(fifo.q))			
			if _len > _offset {
			_removelen := _len - _offset
				DEBUG_OUT("RemovePeeked nextUidOut %d %d %d %d %d %d\n",fifo.nextUidOut, uid, _offset, _len, _fifolen, _removelen)
				DEBUG_OUT("RemovePeeked _removelen %d \n", _removelen)
					if  _removelen >= _fifolen {
						DEBUG_OUT("RemovePeeked (1) nil\n")
				    	fifo.nextUidOut+= _removelen
				    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
					} else {
						DEBUG_OUT("RemovePeeked (2) %d\n",_removelen)
						fifo.nextUidOut += _removelen
						fifo.q = (fifo.q)[_removelen:]
					}
					fifo.mutex.Unlock()
					fifo.condFull.Signal()
					return	
			}					
		}
	}
	DEBUG_OUT("RemovePeeked (3) noop\n")	
	fifo.mutex.Unlock()
	return
}


func (fifo *FIFO) Len() int {
	fifo.mutex.Lock()
	ret := len(fifo.q)
	fifo.mutex.Unlock()
    return ret
}
func (fifo *FIFO) PopOrWait() (n *NODE) {
	n = nil
	DEBUG_OUT(" >>>>>>>>>>>> In PopOrWait (Lock)\n")
	fifo.mutex.Lock()
	_wakeupIter := fifo.wakeupIter
	if(fifo.shutdown) {
		fifo.mutex.Unlock()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWait (Unlock 1)\n")
		return
	}
	if len(fifo.q) > 0 {
	    n = (fifo.q)[0]
	    fifo.q = (fifo.q)[1:]		
	    fifo.nextUidOut++
		fifo.mutex.Unlock()
		fifo.condFull.Signal()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWait (Unlock 2)\n")
		return
	}
	// nothing there, let's wait
	for !fifo.shutdown && fifo.wakeupIter == _wakeupIter {
//		fmt.Printf(" --entering wait %+v\n",*fifo);
	DEBUG_OUT(" ----------- In PopOrWait (Wait / Unlock 1)\n")
		fifo.condWait.Wait() // will unlock it's "Locker" - which is fifo.mutex
//		Wait returns with Lock
//		fmt.Printf(" --out of wait %+v\n",*fifo);
		if fifo.shutdown { 
			fifo.mutex.Unlock()
	DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWait (Unlock 4)\n")
			return 
		}
		if len(fifo.q) > 0 {
		    n = (fifo.q)[0]
		    fifo.q = (fifo.q)[1:]		
		    fifo.nextUidOut++
			fifo.mutex.Unlock()
			fifo.condFull.Signal()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWait (Unlock 3)\n")
			return
			}
	}
	DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWait (Unlock 5)\n")
	fifo.mutex.Unlock()
	return
}
func (fifo *FIFO) PopOrWaitBatch(max uint32) (slice []*NODE) {
	DEBUG_OUT(" >>>>>>>>>>>> In PopOrWaitBatch (Lock)\n")
	fifo.mutex.Lock()
	_wakeupIter := fifo.wakeupIter
	if(fifo.shutdown) {
		fifo.mutex.Unlock()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 1)\n")
		return
	}
	_len := uint32(len(fifo.q))
	if _len > 0 {
		if  max >= _len {
	    	fifo.nextUidOut+= _len
	    	slice = fifo.q
	    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
		} else {
	    	fifo.nextUidOut+= max
			slice = (fifo.q)[0:max]
			fifo.q = (fifo.q)[max:]		
		}
		fifo.mutex.Unlock()
		fifo.condFull.Signal()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 2)\n")
		return
	}
	// nothing there, let's wait
	for !fifo.shutdown && fifo.wakeupIter == _wakeupIter {
//		fmt.Printf(" --entering wait %+v\n",*fifo);
	DEBUG_OUT(" ----------- In PopOrWaitBatch (Wait / Unlock 1)\n")
		fifo.condWait.Wait() // will unlock it's "Locker" - which is fifo.mutex
//		Wait returns with Lock
//		fmt.Printf(" --out of wait %+v\n",*fifo);
		if fifo.shutdown { 
			fifo.mutex.Unlock()
	DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 4)\n")
			return 
		}
		_len = uint32(len(fifo.q))
		if _len > 0 {
			if max >= _len {
		    	fifo.nextUidOut+= _len
		    	slice = fifo.q
		    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
			} else {
		    	fifo.nextUidOut+= max
				slice = (fifo.q)[0:max]
				fifo.q = (fifo.q)[max:]			
			}
			fifo.mutex.Unlock()
			fifo.condFull.Signal()
		DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 3)\n")
			return
		}
	}
	DEBUG_OUT(" <<<<<<<<<<<<< In PopOrWaitBatch (Unlock 5)\n")
	fifo.mutex.Unlock()
	return
}
func (fifo *FIFO) PushOrWait(n *NODE) (ret bool) {
	ret = true
	fifo.mutex.Lock()
	_wakeupIter := fifo.wakeupIter	
    for int(fifo.maxSize) > 0 && (len(fifo.q)+1 > int(fifo.maxSize)) && !fifo.shutdown && (fifo.wakeupIter == _wakeupIter) {
//		fmt.Printf(" --entering push wait %+v\n",*fifo);
    	fifo.condFull.Wait()
		if fifo.shutdown { 
			fifo.mutex.Unlock()
			ret = false
			return
		}    	
//		fmt.Printf(" --exiting push wait %+v\n",*fifo);
    }
    fifo.q = append(fifo.q, n)
    fifo.nextUid++
    fifo.mutex.Unlock()
    fifo.condWait.Signal()
    return
}
func (fifo *FIFO) Shutdown() {
	fifo.mutex.Lock()
	fifo.shutdown = true
	fifo.mutex.Unlock()
	fifo.condWait.Broadcast()
	fifo.condFull.Broadcast()
}
func (fifo *FIFO) WakeupAll() {
	DEBUG_OUT(" >>>>>>>>>>> in WakeupAll @Lock\n")
	fifo.mutex.Lock()
	DEBUG_OUT(" +++++++++++ in WakeupAll\n")
	fifo.wakeupIter++
	fifo.mutex.Unlock()
	DEBUG_OUT(" +++++++++++ in WakeupAll @Unlock\n")
	fifo.condWait.Broadcast()
	fifo.condFull.Broadcast()
	DEBUG_OUT(" <<<<<<<<<<< in WakeupAll past @Broadcast\n")
}
func (fifo *FIFO) IsShutdown() (ret bool) {
	fifo.mutex.Lock()
	ret = fifo.shutdown
	fifo.mutex.Unlock()
	return
}
// end generic






func TestFifo(t *testing.T) {
	buffer := New_testStructFifo(5)
	exits := 0
	// addsome := func(z int, name string){
	// 	for n :=0;n < z;n++ {
	// 		dropped, _ := buffer.Push(new(logBuffer))
	// 		if(dropped) {
	// 			fmt.Printf("[%s] Added and Dropped a buffer!: %d\n",name,n)
	// 		} else {
	// 			fmt.Printf("[%s] added a buffer: %d\n",name,n)
	// 		}
	// 	}		
	// }

	addsome := func(z int, name string){
		for n :=0;n < z;n++ {
			val := new(testStruct)
			val.val = name+strconv.FormatUint(uint64(n),10)
			ok := buffer.PushOrWait(val)
			if(ok) {
				fmt.Printf("[%s] Added a buffer!: %d\n",name,n)
			} else {
				if buffer.IsShutdown() {
					fmt.Printf("[%s] (add buffer) must be shutdown: %d\n",name,n)
					break
				}
			}
		}		
	}

	removesome := func(z int, name string){
		for n :=0;n < z;n++ {
			fmt.Printf("[%s] PopOrWait()...\n",name)
			outbuf := buffer.PopOrWait()
			if(outbuf != nil) {
				fmt.Printf("[%s] Got a buffer (%s)\n",name,outbuf.val)
			} else {
				fmt.Printf("[%s] Got nil - must be shutdown\n",name)
				break
			}
		}
		fmt.Printf("[%s] removesome Done :)\n",name)
		exits++;
	}

	shutdown_in := func(s int) {
		time.Sleep(time.Duration(s)*time.Second)
		fmt.Printf("Shutting down FIFO\n")
		buffer.Shutdown()
		fmt.Printf("Shutdown FIFO complete\n")
	}

	go addsome(10,"one")
	go removesome(10,"remove_one")
	go addsome(10,"two")
	go removesome(10,"remove_two")
	go addsome(10,"three")

	go removesome(11,"remove_three")

	shutdown_in(2)
	time.Sleep(time.Duration(1)*time.Second)
	if exits != 3 {
		fmt.Printf("exits: %d\n",exits)
		panic("Not all exited")
	} 
}

func TestFifoBatch(t *testing.T) {
	buffer := New_testStructFifo(5)
	exits := 0
	// addsome := func(z int, name string){
	// 	for n :=0;n < z;n++ {
	// 		dropped, _ := buffer.Push(new(logBuffer))
	// 		if(dropped) {
	// 			fmt.Printf("[%s] Added and Dropped a buffer!: %d\n",name,n)
	// 		} else {
	// 			fmt.Printf("[%s] added a buffer: %d\n",name,n)
	// 		}
	// 	}		
	// }

	addsome := func(z int, name string){
		for n :=0;n < z;n++ {
			val := new(testStruct)
			val.val = name+strconv.FormatUint(uint64(n),10)
			ok := buffer.PushOrWait(val)
			if(ok) {
				fmt.Printf("[%s] Added a buffer!: %d\n",name,n)
			} else {
				if buffer.IsShutdown() {
					fmt.Printf("[%s] (add buffer) must be shutdown: %d\n",name,n)
					break					
				}
			}
		}		
	}

	removesome := func(z int, b uint32, name string){
//		for n :=0;n < z;n++ {
		for true {
			fmt.Printf("[%s] PopOrWaitBatch(%d)...\n",name,b)
			slice := buffer.PopOrWaitBatch(b)
			fmt.Printf("[%s] PopOrWaitBatch(%d) returned %d\n",name,b,len(slice))
			if slice == nil {
				if(buffer.IsShutdown()) {
					fmt.Printf("[%s] Got shutdown\n",name)
					break
				}				
			}
			for _,outbuf := range slice {
				if(outbuf != nil) {
					fmt.Printf("[%s] Got a buffer (%s)\n",name,outbuf.val)
				} else {
					fmt.Printf("[%s] Got nil - must be shutdown\n",name)
					break
				}				
			}
		}
		fmt.Printf("[%s] removesome Done :)\n",name)
		exits++;
	}

	shutdown_in := func(s int) {
		time.Sleep(time.Duration(s)*time.Second)
		fmt.Printf("Shutting down FIFO\n")
		if buffer.Len() > 0 {
			t.Errorf("Buffer is not empty\n")
		}
		buffer.Shutdown()
		fmt.Printf("Shutdown FIFO complete\n")
	}

	go addsome(10,"one")
	go removesome(3,4,"remove_one")
	go addsome(10,"two")
	go removesome(3,4,"remove_two")
	go addsome(10,"three")

	go removesome(4,4,"remove_three")

	shutdown_in(2)
	time.Sleep(time.Duration(1)*time.Second)
	if exits != 3 {
		fmt.Printf("exits: %d\n",exits)
		t.Errorf("Not all threads exited\n")
	} 
}


func TestFifoBatchNoWait(t *testing.T) {
	buffer := New_testStructFifo(11)
	exits := 0
	// addsome := func(z int, name string){
	// 	for n :=0;n < z;n++ {
	// 		dropped, _ := buffer.Push(new(logBuffer))
	// 		if(dropped) {
	// 			fmt.Printf("[%s] Added and Dropped a buffer!: %d\n",name,n)
	// 		} else {
	// 			fmt.Printf("[%s] added a buffer: %d\n",name,n)
	// 		}
	// 	}		
	// }

	addsome := func(z int, name string){
		for n :=0;n < z;n++ {
			val := new(testStruct)
			val.val = name+strconv.FormatUint(uint64(n),10)
			ok := buffer.PushOrWait(val)
			if(ok) {
				fmt.Printf("[%s] Added a buffer!: %d\n",name,n)
			} else {
				if buffer.IsShutdown() {
					fmt.Printf("[%s] (add buffer) must be shutdown: %d\n",name,n)
					break					
				}
			}
		}		
	}

	removed := 0

	removesomeNoWait := func(z int, b uint32, name string){
		for n :=0;n < z;n++ {
//		for true {
			fmt.Printf("[%s] PopBatch(%d)...\n",name,b)
			slice := buffer.PopBatch(b)
			fmt.Printf("[%s] PopBatch(%d) returned %d\n",name,b,len(slice))
			if slice == nil {
				if(buffer.IsShutdown()) {
					fmt.Printf("[%s] Got shutdown\n",name)
					break
				} else {
					fmt.Printf("[%s] Got nothing.\n",name)
					break
				}
			}
			for _,outbuf := range slice {
				if(outbuf != nil) {
					fmt.Printf("[%s] Got a buffer (%s)\n",name,outbuf.val)
					removed++
				} else {
					fmt.Printf("[%s] Got nil - must be shutdown\n",name)
					break
				}				
			}
		}
		fmt.Printf("[%s] removesomeNoWait Done :)\n",name)
		exits++;
	}

	shutdown_in := func(s int) {
		time.Sleep(time.Duration(s)*time.Second)
		fmt.Printf("Shutting down FIFO\n")
		if buffer.Len() > 0 {
			t.Errorf("Buffer is not empty\n")
		}
		buffer.Shutdown()
		fmt.Printf("Shutdown FIFO complete\n")
	}

	addsome(10,"one")
	go removesomeNoWait(4,3,"one")

	shutdown_in(2)
	time.Sleep(time.Duration(1)*time.Second)
	if removed != 10 {
		fmt.Printf("removed: %d\n",removed)
		t.Errorf("Did not batch deque everything\n")		
	}
	if exits != 1 {
		fmt.Printf("exits: %d\n",exits)
		t.Errorf("Not all threads exited\n")
	} 


}


func TestFifoBatch2(t *testing.T) {
	fmt.Printf("TestFifoBatch2------------------------------------------------------------------------\n")
	buffer := New_testStructFifo(50)
	exits := 0
	// addsome := func(z int, name string){
	// 	for n :=0;n < z;n++ {
	// 		dropped, _ := buffer.Push(new(logBuffer))
	// 		if(dropped) {
	// 			fmt.Printf("[%s] Added and Dropped a buffer!: %d\n",name,n)
	// 		} else {
	// 			fmt.Printf("[%s] added a buffer: %d\n",name,n)
	// 		}
	// 	}		
	// }
	remove_count := uint32(0)
	drop_count := uint32(0)

	addsome := func(z int, b uint32, name string){
		for n :=0;n < z;n++ {
			var q uint32
			slice := make([]*testStruct,b)
			for q =0;q<b;q++ {
				val := new(testStruct)
				val.val = name+strconv.FormatUint(uint64(n),10)+":"+strconv.FormatUint(uint64(q),10)
				slice[q] = val
			}
			haddrop, drops := buffer.PushBatch(slice)
			if(!haddrop) {
				fmt.Printf("[%s] Added a buffers! added: %d\n",name,b)
			} else {
				atomic.AddUint32(&drop_count,uint32(len(drops)))
				fmt.Printf("[%s] Added buffers with drops. added: %d, drops: %d\n",name,n,len(drops))
//				break
			}

		}		
	}

	removesome := func(z int, b uint32, name string){
//		for n :=0;n < z;n++ {
		for true {
			fmt.Printf("[%s] PopOrWaitBatch(%d)...\n",name,b)
			slice := buffer.PopOrWaitBatch(b)
			fmt.Printf("[%s] PopOrWaitBatch(%d) returned %d\n",name,b,len(slice))
			if(slice == nil) {
				if(buffer.IsShutdown()) {
					fmt.Printf("[%s] Got shutdown\n",name)
					break
				}				
			}
			for _,outbuf := range slice {
				if(outbuf != nil) {
					atomic.AddUint32(&remove_count,1)
					fmt.Printf("[%s] Got a buffer (%s)\n",name,outbuf.val)
				} else {
					fmt.Printf("[%s] ?? Got nil\n",name)
					t.Errorf("Should be unreachable")
				}				
			}
		}
		fmt.Printf("[%s] removesome Done :)\n",name)
		exits++;
	}

	total := uint32(3*3*3)

	shutdown_in := func(s int) {
		time.Sleep(time.Duration(s)*time.Second)
		fmt.Printf("Shutting down FIFO\n")
		if buffer.Len() > 0 {
			t.Errorf("Buffer is not empty\n")
		}
		if (remove_count+drop_count) != total {
			t.Errorf("Queue did not handle all test entries: %d vs (%d+%d)\n",total,remove_count,drop_count)
		}
		buffer.Shutdown()
		fmt.Printf("Shutdown FIFO complete\n")
	}



	go addsome(3,3,"one")
	go removesome(3,4,"remove_one")
	go addsome(3,3,"two")
	go removesome(3,4,"remove_two")
	go addsome(3,3,"three")

	go removesome(4,4,"remove_three")

	shutdown_in(2)
	time.Sleep(time.Duration(1)*time.Second)
	if exits != 3 {
		fmt.Printf("exits: %d\n",exits)
		t.Errorf("Not all threads exited\n")
	} 
}


func TestFifoBatch3(t *testing.T) {
	fmt.Printf("TestFifoBatch3------------------------------------------------------------------------\n")
	buffer := New_testStructFifo(10)
	exits := 0
	// addsome := func(z int, name string){
	// 	for n :=0;n < z;n++ {
	// 		dropped, _ := buffer.Push(new(logBuffer))
	// 		if(dropped) {
	// 			fmt.Printf("[%s] Added and Dropped a buffer!: %d\n",name,n)
	// 		} else {
	// 			fmt.Printf("[%s] added a buffer: %d\n",name,n)
	// 		}
	// 	}		
	// }
	remove_count := uint32(0)
	drop_count := uint32(0)

	addsome := func(z int, b uint32, name string){
		for n :=0;n < z;n++ {
			var q uint32
			added := 0
			slice := make([]*testStruct,b)
			for q =0;q<b;q++ {
				val := new(testStruct)
				val.val = name+strconv.FormatUint(uint64(n),10)+":"+strconv.FormatUint(uint64(q),10)
				slice[q] = val
				added++
			}
			fmt.Printf("....Adding %d buffers.\n",len(slice))
			haddrop, drops := buffer.PushBatch(slice)
			if(!haddrop) {
				fmt.Printf("[%s] Added a buffers! added: %d\n",name,added)
			} else {
				atomic.AddUint32(&drop_count,uint32(len(drops)))
				fmt.Printf("[%s] Added buffers with drops. added: %d, drops: %d\n",name,added,len(drops))
//	         break
			}

		}		
	}

	removesome := func(z int, b uint32, name string){
//		for n :=0;n < z;n++ {
		for true {
			fmt.Printf("[%s] PopOrWaitBatch(%d)...\n",name,b)
			slice := buffer.PopOrWaitBatch(b)
			fmt.Printf("[%s] PopOrWaitBatch(%d) returned %d\n",name,b,len(slice))
			if(slice == nil) {
				if(buffer.IsShutdown()) {
					fmt.Printf("[%s] Got shutdown\n",name)
					break
				}				
			}
			for _,outbuf := range slice {
				if(outbuf != nil) {
					atomic.AddUint32(&remove_count,1)
					fmt.Printf("[%s] Got a buffer (%s)\n",name,outbuf.val)
				} else {
					fmt.Printf("[%s] ?? Got nil\n",name)
					t.Errorf("Should be unreachable")
				}				
			}
		}
		fmt.Printf("[%s] removesome Done :)\n",name)
		exits++;
	}

	total := uint32(3*3*3)

	shutdown_in := func(s int) {
		time.Sleep(time.Duration(s)*time.Second)
		fmt.Printf("Shutting down FIFO\n")
		if buffer.Len() > 0 {
			t.Errorf("Buffer is not empty\n")
		}
		if (remove_count+drop_count) != total {
			t.Errorf("Queue did not handle all test entries: %d vs (%d+%d)\n",total,remove_count,drop_count)
		}
		buffer.Shutdown()
		fmt.Printf("Shutdown FIFO complete\n")
	}



	go addsome(3,3,"one")
	go removesome(3,4,"remove_one")
	go addsome(3,3,"two")
	go removesome(3,4,"remove_two")
	go addsome(3,3,"three")

	go removesome(4,4,"remove_three")

	shutdown_in(3)
	time.Sleep(time.Duration(1)*time.Second)
	if exits != 3 {
		fmt.Printf("exits: %d\n",exits)
		t.Errorf("Not all threads exited\n")
	} 
}




func TestFifoPeek1(t *testing.T) {
	fmt.Printf("TestFifoPeek1------------------------------------------------------------------------\n")
	buffer := New_testStructFifo(10)
	exits := 0
	// addsome := func(z int, name string){
	// 	for n :=0;n < z;n++ {
	// 		dropped, _ := buffer.Push(new(logBuffer))
	// 		if(dropped) {
	// 			fmt.Printf("[%s] Added and Dropped a buffer!: %d\n",name,n)
	// 		} else {
	// 			fmt.Printf("[%s] added a buffer: %d\n",name,n)
	// 		}
	// 	}		
	// }
	// remove_count := uint32(0)
	drop_count := uint32(0)

	addsome := func(z int, b uint32, name string){
		for n :=0;n < z;n++ {
			var q uint32
			added := 0
			slice := make([]*testStruct,b)
			for q =0;q<b;q++ {
				val := new(testStruct)
				val.val = name+strconv.FormatUint(uint64(n),10)+":"+strconv.FormatUint(uint64(q),10)
				slice[q] = val
				added++
			}
			fmt.Printf("....Adding %d buffers.\n",len(slice))
			haddrop, drops := buffer.PushBatch(slice)
			if(!haddrop) {
				fmt.Printf("[%s] Added a buffers! added: %d\n",name,added)
			} else {
				atomic.AddUint32(&drop_count,uint32(len(drops)))
				fmt.Printf("[%s] Added buffers with drops. added: %d, drops: %d\n",name,added,len(drops))
//	         break
			}

		}		
	}


	peekonce := func(b uint32, name string, nilok bool) (slice []*testStruct, uid uint32) {
		slice, uid = buffer.PeekBatch(b)
		if slice == nil && !nilok {
			fmt.Printf("Got nil back from PeekBatch")
		} else {
			fmt.Printf("[%s] PeekBatch() Got uid %d and len %d\n",name,uid,len(slice))
		}
		return
	}

	removepeek := func(slice []*testStruct, uid uint32, name string){
		fmt.Printf("[%s] RemovePeeked(%d)\n",name,uid)
		buffer.RemovePeeked(slice, uid)
		return
	}

	// total := uint32(3*3*3)

	shutdown_in := func(s int) {
		time.Sleep(time.Duration(s)*time.Second)
		fmt.Printf("Shutting down FIFO\n")
		if buffer.Len() > 0 {
			t.Errorf("Buffer is not empty: %d\n", buffer.Len())
		}
		// if (remove_count+drop_count) != total {
		// 	t.Errorf("Queue did not handle all test entries: %d vs (%d+%d)\n",total,remove_count,drop_count)
		// }
		buffer.Shutdown()
		fmt.Printf("Shutdown FIFO complete\n")
	}



	addsome(3,3,"one")
	if buffer.Len() != 9 {
		t.Errorf("In corrent length! (1)\n")
	}
	slice, uid := peekonce(3,"one",false)
	removepeek(slice,uid,"one")
	if buffer.Len() != 6 {
		t.Errorf("In corrent length! (2) was %d\n",buffer.Len())
	}
	slice, uid = peekonce(3,"one",false)
	removepeek(slice,uid,"one")
	if buffer.Len() != 3 {
		t.Errorf("In corrent length! (2) was %d\n",buffer.Len())
	}
	slice, uid = peekonce(3,"one",false)
	removepeek(slice,uid,"one")

	// test for nothing
	slice, uid = peekonce(3,"one",true)
	removepeek(slice,uid,"one")
	if len(slice) != 0 {
		t.Errorf("In corrent length! should be zero.\n")
	}

	addsome(3,3,"two")
	if buffer.Len() != 9 {
		t.Errorf("In corrent length! (1)\n")
	}

	// we see the first 3 in the 9 length queue

	slice, uid = peekonce(3,"two",false)
	fmt.Printf("peek: %+v , %d \n",slice,uid)
	addsome(1,3,"three")
	if buffer.Len() != 10 { // 10 is the max
		t.Errorf("In corrent length! (1) %d\n",buffer.Len())
	}

	// now have 10 in the queue...

	slice2, uid2 := peekonce(3,"two",false)
	fmt.Printf("peek2: %+v , %d \n",slice2,uid2)
	// this should effectively remove 3
	removepeek(slice2,uid2,"peek2")

	if buffer.Len() != 7 {
		t.Errorf("In corrent length! removepeek() %d\n",buffer.Len())
	}
	// this should effectively remove none, b/c the ones that this Peek referred to were bumped off the 
	// end to make room for new ones.
	removepeek(slice,uid,"peek1")
	if buffer.Len() != 7{
		t.Errorf("In corrent length! [2] removepeek() %d\n",buffer.Len())
	}

	slice, uid = peekonce(7,"two",false)
	removepeek(slice,uid,"peek3")	

	if buffer.Len() != 0 {
		t.Errorf("In corrent length! [2] removepeek() %d\n",buffer.Len())
	}

	// peek and get nothing
	slice, uid = peekonce(2,"two",false)
	if len(slice) != 0 {
		t.Errorf("Peek slice should be zero length.\n")
	}
	removepeek(slice,uid,"peek4")	


	shutdown_in(3)
	time.Sleep(time.Duration(1)*time.Second)
	if exits != 0 {
		fmt.Printf("exits: %d\n",exits)
		t.Errorf("Not all threads exited\n")
	} 
}
