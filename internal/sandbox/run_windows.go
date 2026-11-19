//go:build windows && amd64

package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"skrinpolka/internal/sandboxwin"
)

var windowsWorkerGate sync.Mutex
var windowsWorkerStopProblem error

func resources(spec Spec)error{
	memory,err:=sandboxwin.MemoryNow();if err!=nil{return err}
	if memory.RAMAvailable<spec.MemoryBytes+1<<30||memory.CommitAvailable<spec.MemoryBytes+512<<20{return fmt.Errorf("Недостаточно свободной RAM/commit для worker и резерва системы")}
	disk,err:=sandboxwin.DiskFree(spec.Directory);if err!=nil{return err};if disk<256<<20{return fmt.Errorf("Для обработки нужно не менее 256 MiB свободного места")};return nil
}

func run(ctx context.Context,spec Spec)(resultErr error){
	windowsWorkerGate.Lock()
	defer windowsWorkerGate.Unlock()
	if windowsWorkerStopProblem!=nil{return fmt.Errorf("Остановка прежнего worker не подтверждена; новые задания отключены до перезапуска приложения: %w",windowsWorkerStopProblem)}
	if err:=ctx.Err();err!=nil{return err}
	if err:=sandboxwin.PrivateDirectory(spec.Directory);err!=nil{return fmt.Errorf("Не удалось защитить временный каталог: %w",err)}
	var id [16]byte;if _,err:=rand.Read(id[:]);err!=nil{return err}
	container,err:=sandboxwin.NewContainer(ctx,"Skrinpolka.Worker."+hex.EncodeToString(id[:]),spec.ReadRoots,spec.Directory)
	if err!=nil{return fmt.Errorf("Не удалось создать AppContainer: %w",err)}
	defer func(){resultErr=errors.Join(resultErr,container.Close())}()
	if err:=ctx.Err();err!=nil{return err}
	// Windows needs LOCALAPPDATA when creating an AppContainer process, even
	// for a command-line worker. Keep its application data inside this private
	// task directory instead of inheriting the owner's profile/environment.
	environment:=[]string{"SystemRoot="+os.Getenv("SystemRoot"),"WINDIR="+os.Getenv("SystemRoot"),"LOCALAPPDATA="+spec.Directory,"TEMP="+spec.Directory,"TMP="+spec.Directory,"OMP_THREAD_LIMIT="+strconv.Itoa(spec.Threads),"OMP_NUM_THREADS="+strconv.Itoa(spec.Threads),"OPENBLAS_NUM_THREADS="+strconv.Itoa(spec.Threads),"HF_HUB_OFFLINE=1","TRANSFORMERS_OFFLINE=1"}
	process,err:=sandboxwin.Start(spec.Executable,spec.Arguments,environment,spec.Directory,spec.MemoryBytes,container)
	if err!=nil{return err}
	defer func(){if err:=process.Close();err!=nil{windowsWorkerStopProblem=err;resultErr=errors.Join(resultErr,err)}}()
	if err:=ctx.Err();err!=nil{return err}
	if err:=process.Resume();err!=nil{return err}
	ticker:=time.NewTicker(50*time.Millisecond);defer ticker.Stop()
	for{select{
	case <-ctx.Done():return errors.Join(ctx.Err(),process.Kill())
	case <-ticker.C:done,code,err:=process.Poll();if err!=nil{return err};if done{if code!=0{return fmt.Errorf("Локальный worker завершился с кодом %d",code)};return nil}
	}}
}
