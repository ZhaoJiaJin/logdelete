package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type LogCfg struct {
	sync.RWMutex
	logmap map[string][]string //{disk:path}
}

type MyFile struct {
	name    string
	modTime time.Time
}

type MyFileList []MyFile

var (
	NoFileSysErr = errors.New("can not get file sys info")
	TypeCvtErr   = errors.New("type convert error")
	NotDirErr    = errors.New("path is not Dir")

	reloadcfginterval        = 10 * time.Second
	checkinterval            = 30 * time.Second
	freeperc          uint64 = 10
	//freeperc uint64 = 80
	delStopAt uint64 = 20
	//delStopAt uint64 = 90
	oldfile = 12 * time.Hour
	//oldfile = 12 * time.Second

	debug    bool
	cfgfile  string
	cfgmtime time.Time
	cfg      LogCfg
)

func (l MyFileList) Len() int {
	return len(l)
}
func (l MyFileList) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}
func (l MyFileList) Less(i, j int) bool {
	return l[i].modTime.Before(l[j].modTime)
}
func getAccTime(info os.FileInfo) (time.Time, error) {
	var ret time.Time
	sysinfo := info.Sys()
	if sysinfo == nil {
		return ret, NoFileSysErr
	}
	sysstat, ok := sysinfo.(*syscall.Stat_t)
	if !ok {
		return ret, TypeCvtErr
	}
	return time.Unix(sysstat.Atim.Sec, sysstat.Atim.Nsec), nil
}

func getModTime(info os.FileInfo) time.Time {
	return info.ModTime()
}

func leadwith(s1, s2 string) bool {
	if len(s1) < len(s2) {
		return false
	}
	return s1[:len(s2)] == s2
}

func isDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return NotDirErr
	}
	return nil
}

func isExist(list []string, rec string) bool {
	for _, v := range list {
		if v == rec {
			return true
		}
	}
	return false
}

func buildcfgmap(cfgstr string, mountpts []string) {
	cfg.Lock()
	defer cfg.Unlock()
	cfg.logmap = make(map[string][]string)
	for _, logpath := range strings.Split(cfgstr, "\n") {
		if len(logpath) == 0 {
			continue
		}
		if logpath[0] != '/' {
			log.Println("[ERROR]路径必须是绝对路径:", logpath)
			continue
		}
		err := isDir(logpath)
		if err != nil {
			log.Println("[ERROR]:", logpath, err)
			continue
		}
		//build map
		for _, mp := range mountpts {
			if leadwith(logpath, mp) {
				_, present := cfg.logmap[mp]
				if !present {
					cfg.logmap[mp] = make([]string, 0)
				}
				if !isExist(cfg.logmap[mp], logpath) {
					cfg.logmap[mp] = append(cfg.logmap[mp], logpath)
					log.Println("[add]add", logpath, "to mount point", mp)
				}
				break
			}
		}
	}
}

func loadcfg() error {
	cfginfo, err := os.Stat(cfgfile)
	if err != nil {
		return err
	}
	newmtime := getModTime(cfginfo)
	if newmtime == cfgmtime {
		return nil
	}
	cfgmtime = newmtime
	log.Println("update config from file:", cfgfile)
	dat, err := ioutil.ReadFile(cfgfile)
	if err != nil {
		return err
	}
	mountpints, err := getmountpoint()
	if err != nil {
		return err
	}
	buildcfgmap(string(dat), mountpints)
	return nil
}

func reloadcfg() {
	for {
		time.Sleep(reloadcfginterval)
		err := loadcfg()
		if err != nil {
			log.Panic(err)
		}
	}
}

func getmountpoint() ([]string, error) {
	dat, err := ioutil.ReadFile("/proc/mounts")
	if err != nil {
		return []string{}, err
	}
	recs := strings.Split(string(dat), "\n")
	ans := make([]string, 0)
	for i := 0; i < len(recs); i++ {
		sps := strings.Split(recs[i], " ")
		if len(sps) > 2 && strings.Contains(sps[0], "/dev") {
			ans = append(ans, sps[1])
		}
	}
	for i := 0; i < len(ans); i++ {
		for j := i + 1; j < len(ans); j++ {
			if len(ans[i]) < len(ans[j]) {
				ans[i], ans[j] = ans[j], ans[i]
			}
		}
	}
	return ans, nil
}

func canDelete(logpath string, info os.FileInfo, pathname string, openfiles map[string][]string) bool {
	//modify time older then 12h
	if time.Now().Sub(getModTime(info)) < oldfile {
		return false
	}
	//access time older then 12h
	acctime, err := getAccTime(info)
	if err != nil {
		log.Println("[ERROR]get file access time err", info.Name(), err)
	} else {
		if time.Now().Sub(acctime) < oldfile {
			return false
		}
	}
	//not open by other program
	pathopenfiles, present := openfiles[logpath]
	if !present { //no file in this path is open
		return true
	}
	for _, openf := range pathopenfiles {
		if openf == pathname {
			//file open
			return false
		}
	}
	return true
}

func dellog(disk string, logpaths []string, openfiles map[string][]string) {
	log.Println("[info]clear for", disk)
	var loglist MyFileList
	for _, logpath := range logpaths {
		filepath.Walk(logpath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Println("[ERROR]walk file error", path, err)
				return err
			}
			if logpath == path {
				return nil
			}
			if info.IsDir() {
				return filepath.SkipDir
			}
			if canDelete(logpath, info, path, openfiles) {
				loglist = append(loglist, MyFile{path, getModTime(info)})
			}
			return nil
		})
	}
	deleteAndCheck(loglist, disk)
}

func deleteAndCheck(loglist MyFileList, disk string) {
	if len(loglist) == 0 {
		log.Println("[WARN]nothing to delete")
		return
	}
	//sort first
	sort.Sort(loglist)
	//del from begin
	for len(loglist) > 0 {
		firstlog := loglist[0]
		loglist = loglist[1:]
		if debug {
			log.Println("[info]fake delete file:", firstlog.name)
		} else {
			log.Println("[info]delete file:", firstlog.name)
			err := os.Remove(firstlog.name)
			if err != nil {
				log.Println("[ERROR]error when delete file:", err)
			}
		}
		perc := diskperc(disk)
		if perc > delStopAt {
			log.Println("[info]delete done,", disk, perc, "% free")
			return
		}
	}
	log.Println("[WARN]nothing more to delete")
}

func diskperc(disk string) uint64 {
	var stat syscall.Statfs_t
	err := syscall.Statfs(disk, &stat)
	if err != nil {
		log.Panic(err)
	}
	return stat.Bfree * 100 / uint64(stat.Blocks)
}

func getopenfile() map[string][]string {
	procdir := "/proc"
	proclist := make([]string, 0)
	//get process list
	filepath.Walk(procdir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("[ERROR]walk file error", path, err)
			return nil
		}
		if path == procdir {
			return nil
		}
		pid, numerr := strconv.Atoi(info.Name())
		selfpid := os.Getpid()
		_, fderr := os.Stat(path + "/fd")
		if info.IsDir() && numerr == nil && pid != selfpid && fderr == nil {
			proclist = append(proclist, path+"/fd")
		}
		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	//build open file map
	openmap := make(map[string][]string) //logpath:[file1,file2]
	for _, fdpath := range proclist {
		filepath.Walk(fdpath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				log.Println("[WARN]walk file error", path, err)
				return nil
			}
			if fdpath == path {
				return nil
			}
			if info.IsDir() {
				return filepath.SkipDir
			}
			islink, err := isSymlink(path)
			if err != nil {
				log.Println("[WARN]walk file error", path, err)
				return nil
			}
			if islink {
				realfile, err := os.Readlink(path)
				if err != nil {
					log.Println("[ERROR]walk file error", path, err)
					return nil
				}
				for _, logpaths := range cfg.logmap {
					find := false
					for _, logpath := range logpaths {
						if leadwith(realfile, logpath) {
							find = true
							_, present := openmap[logpath]
							if !present {
								openmap[logpath] = make([]string, 0)
							}
							openmap[logpath] = append(openmap[logpath], realfile)
						}
					}
					if find {
						break
					}
				}
			}
			return nil
		})
	}
	return openmap
}

func isSymlink(name string) (bool, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

func checkroutine() {
	for {
		cfg.RLock()
		//then delete file
		for disk, logpaths := range cfg.logmap {
			perc := diskperc(disk)
			if perc < freeperc { //free percentage
				//get open file first
				openfiles := getopenfile()
				log.Println("[info]disk free space ", disk, perc, "%, begin delete")
				dellog(disk, logpaths, openfiles)
			} else {
				log.Println("[info]disk free space ", disk, perc, "%, don't need delete")
			}
		}
		cfg.RUnlock()
		time.Sleep(checkinterval)
	}
}

func main() {
	flag.StringVar(&cfgfile, "c", "config", "config file")
	flag.BoolVar(&debug, "d", false, "debug mode")
	flag.Parse()
	log.Println("using config file:", cfgfile)
	if debug {
		log.Println("[info]enable debug mode")
	}
	cfg.logmap = make(map[string][]string)
	err := loadcfg()
	if err != nil {
		log.Panic(err)
	}
	go reloadcfg()
	checkroutine()
}
