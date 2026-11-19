package core

import (
	"encoding/hex"
	"fmt"
	"os"
)

// ScreenRegion stores physical desktop pixels, including the virtual desktop
// origin so a monitor/resolution change cannot silently select another area.
type ScreenRegion struct {
	X int `json:"x"`
	Y int `json:"y"`
	Width int `json:"width"`
	Height int `json:"height"`
	DesktopX int `json:"desktop_x"`
	DesktopY int `json:"desktop_y"`
	DesktopWidth int `json:"desktop_width"`
	DesktopHeight int `json:"desktop_height"`
	DesktopTopology string `json:"desktop_topology,omitempty"`
}

func (r ScreenRegion) Validate() error {
	if r.DesktopTopology!="" {
		if len(r.DesktopTopology)!=64{return fmt.Errorf("Некорректная ревизия дисплеев тихой области")}
		if _,err:=hex.DecodeString(r.DesktopTopology);err!=nil{return fmt.Errorf("Некорректная ревизия дисплеев тихой области")}
	}
	for _, value := range []int{r.X,r.Y,r.DesktopX,r.DesktopY} {
		if value < -1_000_000 || value > 1_000_000 { return fmt.Errorf("Некорректные координаты тихой области") }
	}
	if r.Width<=0||r.Height<=0||r.Width>MaxPixels||r.Height>MaxPixels||int64(r.Width)*int64(r.Height)>MaxPixels { return fmt.Errorf("Тихая область должна содержать от 1 до 16 млн пикселей") }
	if r.DesktopWidth<=0||r.DesktopHeight<=0||r.DesktopWidth>32_000_000||r.DesktopHeight>32_000_000||int64(r.DesktopWidth)*int64(r.DesktopHeight)>32_000_000 { return fmt.Errorf("Рабочий стол превышает лимит захвата 32 млн пикселей") }
	if r.X<r.DesktopX||r.Y<r.DesktopY||int64(r.X)+int64(r.Width)>int64(r.DesktopX)+int64(r.DesktopWidth)||int64(r.Y)+int64(r.Height)>int64(r.DesktopY)+int64(r.DesktopHeight) { return fmt.Errorf("Тихая область выходит за границы рабочего стола") }
	return nil
}

func (r ScreenRegion) MatchesDesktop(x,y,width,height int) bool {
	return r.DesktopX==x&&r.DesktopY==y&&r.DesktopWidth==width&&r.DesktopHeight==height
}

func (a *App) SaveSilentRegion(region ScreenRegion) (ScreenRegion,error) {
	if err:=region.Validate();err!=nil{return ScreenRegion{},err}
	a.mu.Lock();defer a.mu.Unlock()
	settings:=a.settings;settings.SilentRegion=&region
	root,err:=os.OpenRoot(a.dataRoot);if err!=nil{return ScreenRegion{},err};defer root.Close()
	if err:=saveSettingsFile(root,settings);err!=nil{return ScreenRegion{},err}
	a.settings=settings
	return region,nil
}
