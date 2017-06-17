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

//  begin generic
// m4_define({{*NODE*}},{{*testStruct*}})  m4_define({{*FIFO*}},{{*testStructFifo*}})  
// Thread safe queue for LogBuffer
type FIFO struct {
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
    	DEBUG_OUT("!!! Dropping NODE in FIFO \n")
    	drop = true
    }
    fifo.q = append(fifo.q, n)
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
    	} else if needdrop > 0 {
	    	drop = true
	    	dropped = (fifo.q)[0:needdrop]
	    	fifo.q=(fifo.q)[needdrop:]
	    }
    	// // drop off the queue
    	// dropped = (fifo.q)[0]
    	// fifo.q = (fifo.q)[1:]
    	// fifo.drops++
    	DEBUG_OUT(" ----------- PushBatch() !!! Dropping %d NODE in FIFO \n", len(dropped))
    }
    DEBUG_OUT(" ----------- In PushBatch (pushed %d)\n",_inlen)
    fifo.q = append(fifo.q, n[0:int(_inlen)]...)
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
	    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
		} else {
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

func PeekBatch(max uint32) (slice []*NODE) {
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
	    	slice = fifo.q
	    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
		} else {
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
		    	slice = fifo.q
		    	fifo.q = nil  // http://stackoverflow.com/questions/29164375/golang-correct-way-to-initialize-empty-slice
			} else {
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
