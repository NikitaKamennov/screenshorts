//go:build !linux && (!windows || !amd64)

package sandbox

import("context";"fmt")

func resources(Spec)error{return fmt.Errorf("Защищённые workers поддержаны в Linux и Windows x64")}
func run(context.Context,Spec)error{return fmt.Errorf("Нет защищённого адаптера для этой платформы")}
