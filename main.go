package main

import(
	"os"
	"log"
	"syscall"
	"time"
	"errors"
	"flag"
)

var(
	NoFileSysErr = errors.New("can not get file sys info")
	TypeCvtErr = errors.New("type convert error")
)



func getAccTime(info os.FileInfo)(time.Time,error){
	var ret time.Time
	sysinfo := info.Sys()
	if sysinfo == nil{
		return ret,NoFileSysErr
	}
	sysstat, ok := sysinfo.(*syscall.Stat_t)
	if !ok{
		return ret,TypeCvtErr
	}
	return time.Unix(sysstat.Atim.Sec,sysstat.Atim.Nsec),nil
}

func getModTime(info os.FileInfo)(time.Time){
	return info.ModTime()
}

func main(){
	var cfgfile string
	flag.StringVar(&cfgfile,"c","config","config file")
	flag.Parse()
	log.Println("using config file:",cfgfile)
}
