package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const modrinthAPI = "https://api.modrinth.com/v2"
const curseAPI = "https://api.curseforge.com/v1"

type SearchHit struct {
	Source      string   `json:"source"`
	ID          string   `json:"id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Author      string   `json:"author"`
	Downloads   int64    `json:"downloads"`
	Follows     int64    `json:"follows"`
	Categories  []string `json:"categories"`
	URL         string   `json:"url"`
	Updated     string   `json:"updated"`
}

type SearchResult struct {
	Hits  []SearchHit `json:"hits"`
	Total int         `json:"total"`
}

func loaderSet(loader, mc string) []string {
	switch loader {
	case "quilt":
		return []string{"quilt", "fabric"}
	case "neoforge":
		if mc == "1.20.1" {
			return []string{"neoforge", "forge"}
		}
		return []string{"neoforge"}
	case "", "vanilla":
		return []string{}
	}
	return []string{loader}
}

func jsonArr(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func searchMods(source, q, mc, loader, sort string, offset int, projectType string) (*SearchResult, error) {
	if projectType == "" {
		projectType = "mod"
	}
	if source == "curseforge" {
		return searchCurse(q, mc, loader, sort, offset, projectType)
	}
	facets := [][]string{{"project_type:" + projectType}}
	if mc != "" {
		facets = append(facets, []string{"versions:" + mc})
	}
	if projectType == "mod" {
		if ls := loaderSet(loader, mc); len(ls) > 0 {
			var f []string
			for _, l := range ls {
				f = append(f, "categories:"+l)
			}
			facets = append(facets, f)
		}
	}
	fb, _ := json.Marshal(facets)
	if sort == "" {
		sort = "relevance"
	}
	u := fmt.Sprintf("%s/search?query=%s&facets=%s&index=%s&offset=%d&limit=24",
		modrinthAPI, url.QueryEscape(q), url.QueryEscape(string(fb)), sort, offset)
	var r struct {
		Hits []struct {
			ProjectID   string   `json:"project_id"`
			Slug        string   `json:"slug"`
			Title       string   `json:"title"`
			Description string   `json:"description"`
			IconURL     string   `json:"icon_url"`
			Author      string   `json:"author"`
			Downloads   int64    `json:"downloads"`
			Follows     int64    `json:"follows"`
			Categories  []string `json:"display_categories"`
			Modified    string   `json:"date_modified"`
			ProjectType string   `json:"project_type"`
		} `json:"hits"`
		Total int `json:"total_hits"`
	}
	if err := getJSON(u, &r); err != nil {
		return nil, err
	}
	out := &SearchResult{Total: r.Total, Hits: []SearchHit{}}
	for _, h := range r.Hits {
		out.Hits = append(out.Hits, SearchHit{
			Source: "modrinth", ID: h.ProjectID, Slug: h.Slug, Title: h.Title, Description: h.Description,
			Icon: h.IconURL, Author: h.Author, Downloads: h.Downloads, Follows: h.Follows, Categories: h.Categories,
			URL: "https://modrinth.com/" + h.ProjectType + "/" + h.Slug, Updated: h.Modified,
		})
	}
	return out, nil
}

func curseLoaderType(loader string) string {
	switch loader {
	case "forge":
		return "1"
	case "fabric":
		return "4"
	case "quilt":
		return "5"
	case "neoforge":
		return "6"
	}
	return ""
}

func curseHeaders() (map[string]string, error) {
	k := strings.TrimSpace(store.Settings().CurseForgeKey)
	if k == "" {
		return nil, fmt.Errorf("adicione sua chave da API CurseForge nas configurações")
	}
	return map[string]string{"x-api-key": k}, nil
}

func searchCurse(q, mc, loader, sort string, offset int, projectType string) (*SearchResult, error) {
	h, err := curseHeaders()
	if err != nil {
		return nil, err
	}
	sf := "2"
	switch sort {
	case "downloads":
		sf = "6"
	case "updated", "newest":
		sf = "3"
	case "follows":
		sf = "2"
	case "relevance":
		sf = "2"
		if q != "" {
			sf = "2"
		}
	}
	classID := "6"
	switch projectType {
	case "resourcepack":
		classID = "12"
	case "shader":
		classID = "6552"
	}
	v := url.Values{"gameId": {"432"}, "classId": {classID}, "searchFilter": {q}, "sortField": {sf}, "sortOrder": {"desc"},
		"index": {strconv.Itoa(offset)}, "pageSize": {"24"}}
	if mc != "" {
		v.Set("gameVersion", mc)
	}
	if lt := curseLoaderType(loader); lt != "" && projectType == "mod" {
		v.Set("modLoaderType", lt)
	}
	var r struct {
		Data []struct {
			ID            int64   `json:"id"`
			Name          string  `json:"name"`
			Slug          string  `json:"slug"`
			Summary       string  `json:"summary"`
			DownloadCount float64 `json:"downloadCount"`
			ThumbsUp      int64   `json:"thumbsUpCount"`
			DateModified  string  `json:"dateModified"`
			Logo          *struct {
				ThumbnailURL string `json:"thumbnailUrl"`
				URL          string `json:"url"`
			} `json:"logo"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
			Categories []struct {
				Name string `json:"name"`
			} `json:"categories"`
			Links struct {
				WebsiteURL string `json:"websiteUrl"`
			} `json:"links"`
		} `json:"data"`
		Pagination struct {
			TotalCount int `json:"totalCount"`
		} `json:"pagination"`
	}
	if err := getJSON(curseAPI+"/mods/search?"+v.Encode(), &r, h); err != nil {
		return nil, err
	}
	out := &SearchResult{Total: r.Pagination.TotalCount, Hits: []SearchHit{}}
	if out.Total > 10000 {
		out.Total = 10000
	}
	for _, d := range r.Data {
		hit := SearchHit{Source: "curseforge", ID: strconv.FormatInt(d.ID, 10), Slug: d.Slug, Title: d.Name,
			Description: d.Summary, Downloads: int64(d.DownloadCount), Follows: d.ThumbsUp, URL: d.Links.WebsiteURL, Updated: d.DateModified}
		if d.Logo != nil {
			hit.Icon = d.Logo.ThumbnailURL
			if hit.Icon == "" {
				hit.Icon = d.Logo.URL
			}
		}
		if len(d.Authors) > 0 {
			hit.Author = d.Authors[0].Name
		}
		for _, c := range d.Categories {
			hit.Categories = append(hit.Categories, c.Name)
		}
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

type mrVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		VersionID      string `json:"version_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

type mrProject struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	ProjectType string `json:"project_type"`
}

func (v *mrVersion) primary() (string, string, string, int64) {
	if len(v.Files) == 0 {
		return "", "", "", 0
	}
	f := v.Files[0]
	for _, x := range v.Files {
		if x.Primary {
			f = x
		}
	}
	return f.URL, f.Filename, f.Hashes["sha1"], f.Size
}

type InstallReq struct {
	Source      string `json:"source"`
	ProjectID   string `json:"projectId"`
	VersionID   string `json:"versionId"`
	ProjectType string `json:"projectType"`
}

func installRemote(id string, req InstallReq) error {
	inst, ok := store.Get(id)
	if !ok {
		return errNotFound
	}
	t := NewTask("Instalando mod", id)
	visited := map[string]bool{}
	var err error
	if req.Source == "curseforge" {
		err = installCurse(t, inst, req.ProjectID, req.VersionID, false, visited, req.ProjectType)
	} else {
		err = installModrinth(t, inst, req.ProjectID, req.VersionID, false, visited, req.ProjectType)
	}
	t.Finish(err)
	hub.Emit("mods", id)
	return err
}

func targetDir(id, projectType string) string {
	switch projectType {
	case "resourcepack":
		return filepath.Join(store.GameDir(id), "resourcepacks")
	case "shader":
		return filepath.Join(store.GameDir(id), "shaderpacks")
	}
	return modsDir(id)
}

func installModrinth(t *Task, inst Instance, projectID, versionID string, dep bool, visited map[string]bool, ptype string) error {
	if visited[projectID] {
		return nil
	}
	visited[projectID] = true
	var proj mrProject
	if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(projectID), &proj); err != nil {
		return err
	}
	if ptype == "" {
		ptype = proj.ProjectType
	}
	t.SetDetail(proj.Title)
	if dep {

		for _, m := range inst.Mods {
			if m.ProjectID == proj.ID {
				return nil
			}
		}
	}
	var ver mrVersion
	if versionID != "" {
		if err := getJSON(modrinthAPI+"/version/"+versionID, &ver); err != nil {
			return err
		}
	} else {
		q := url.Values{}
		if ptype == "mod" {
			if ls := loaderSet(inst.Loader, inst.MCVersion); len(ls) > 0 {
				q.Set("loaders", jsonArr(ls))
			}
		}
		q.Set("game_versions", jsonArr([]string{inst.MCVersion}))
		var vs []mrVersion
		if err := getJSON(modrinthAPI+"/project/"+proj.ID+"/version?"+q.Encode(), &vs); err != nil {
			return err
		}
		if len(vs) == 0 {
			return fmt.Errorf("%s não tem versão para %s %s", proj.Title, loaderLabel(inst.Loader), inst.MCVersion)
		}
		ver = vs[0]
		for _, v := range vs {
			if v.VersionType == "release" {
				ver = v
				break
			}
		}
	}
	u, fname, sha, size := ver.primary()
	if u == "" {
		return fmt.Errorf("%s: versão sem arquivo", proj.Title)
	}
	dir := targetDir(inst.ID, ptype)
	t.Stage("Baixando "+proj.Title, size)
	if err := downloadFile(DL{URL: u, Path: filepath.Join(dir, fname), SHA1: sha, Size: size}, t.Add); err != nil {
		return err
	}
	if ptype == "mod" {
		removeOtherFilesOfProject(inst.ID, proj.ID, fname)
		store.Update(inst.ID, func(in *Instance) error {
			cat := ""
			if old := in.Mods[fname]; old != nil {
				cat = old.Category
			}
			in.Mods[fname] = &ModMeta{Source: "modrinth", ProjectID: proj.ID, VersionID: ver.ID, Slug: proj.Slug,
				Title: proj.Title, Summary: proj.Description, IconURL: proj.IconURL,
				PageURL: "https://modrinth.com/mod/" + proj.Slug, Checked: true, Added: time.Now().Unix(), SHA1: sha,
				Dependency: dep, Category: cat}
			in.activeList().Enabled[fname] = true
			os.Remove(filepath.Join(modsDir(in.ID), fname+".disabled"))
			return nil
		})
		inst, _ = store.Get(inst.ID)
		for _, d := range ver.Dependencies {
			if d.DependencyType != "required" || d.ProjectID == "" {
				continue
			}
			if err := installModrinth(t, inst, d.ProjectID, "", true, visited, "mod"); err != nil {
				appendLog(inst.ID, "[Hypercube] dependência: "+err.Error())
			}
			inst, _ = store.Get(inst.ID)
		}
	}
	return nil
}

func removeOtherFilesOfProject(id, projectID, keep string) {
	files := listModFiles(id)
	store.Update(id, func(in *Instance) error {
		for k, m := range in.Mods {
			if k != keep && m.ProjectID == projectID {
				if f, ok := files[k]; ok {
					os.Remove(filepath.Join(modsDir(id), f))
				}
				delete(in.Mods, k)
				for i := range in.Modlists {
					delete(in.Modlists[i].Enabled, k)
				}
			}
		}
		return nil
	})
}

func loaderLabel(l string) string {
	switch l {
	case "fabric":
		return "Fabric"
	case "forge":
		return "Forge"
	case "neoforge":
		return "NeoForge"
	case "quilt":
		return "Quilt"
	}
	return "Vanilla"
}

type cfFile struct {
	ID          int64  `json:"id"`
	ModID       int64  `json:"modId"`
	FileName    string `json:"fileName"`
	DownloadURL string `json:"downloadUrl"`
	FileLength  int64  `json:"fileLength"`
	Hashes      []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
	Dependencies []struct {
		ModID        int64 `json:"modId"`
		RelationType int   `json:"relationType"`
	} `json:"dependencies"`
}

func installCurse(t *Task, inst Instance, projectID, fileID string, dep bool, visited map[string]bool, ptype string) error {
	if visited[projectID] {
		return nil
	}
	visited[projectID] = true
	h, err := curseHeaders()
	if err != nil {
		return err
	}
	if dep {
		for _, m := range inst.Mods {
			if m.Source == "curseforge" && m.ProjectID == projectID {
				return nil
			}
		}
	}
	var mod struct {
		Data struct {
			ID      int64  `json:"id"`
			Name    string `json:"name"`
			Slug    string `json:"slug"`
			Summary string `json:"summary"`
			ClassID int    `json:"classId"`
			Logo    *struct {
				ThumbnailURL string `json:"thumbnailUrl"`
			} `json:"logo"`
			Links struct {
				WebsiteURL string `json:"websiteUrl"`
			} `json:"links"`
		} `json:"data"`
	}
	if err := getJSON(curseAPI+"/mods/"+projectID, &mod, h); err != nil {
		return err
	}
	if ptype == "" {
		switch mod.Data.ClassID {
		case 12:
			ptype = "resourcepack"
		case 6552:
			ptype = "shader"
		default:
			ptype = "mod"
		}
	}
	t.SetDetail(mod.Data.Name)
	var file cfFile
	if fileID != "" {
		var r struct {
			Data cfFile `json:"data"`
		}
		if err := getJSON(curseAPI+"/mods/"+projectID+"/files/"+fileID, &r, h); err != nil {
			return err
		}
		file = r.Data
	} else {
		v := url.Values{"gameVersion": {inst.MCVersion}, "pageSize": {"20"}}
		if lt := curseLoaderType(inst.Loader); lt != "" && ptype == "mod" {
			v.Set("modLoaderType", lt)
		}
		var r struct {
			Data []cfFile `json:"data"`
		}
		if err := getJSON(curseAPI+"/mods/"+projectID+"/files?"+v.Encode(), &r, h); err != nil {
			return err
		}
		if len(r.Data) == 0 {
			return fmt.Errorf("%s não tem arquivo para %s %s", mod.Data.Name, loaderLabel(inst.Loader), inst.MCVersion)
		}
		file = r.Data[0]
	}
	dl := file.DownloadURL
	if dl == "" {
		dl = fmt.Sprintf("https://edge.forgecdn.net/files/%d/%d/%s", file.ID/1000, file.ID%1000, url.PathEscape(file.FileName))
	}
	sha := ""
	for _, hh := range file.Hashes {
		if hh.Algo == 1 {
			sha = hh.Value
		}
	}
	dir := targetDir(inst.ID, ptype)
	t.Stage("Baixando "+mod.Data.Name, file.FileLength)
	if err := downloadFile(DL{URL: dl, Path: filepath.Join(dir, file.FileName), SHA1: sha, Size: file.FileLength}, t.Add); err != nil {
		return err
	}
	if ptype != "mod" {
		return nil
	}
	icon := ""
	if mod.Data.Logo != nil {
		icon = mod.Data.Logo.ThumbnailURL
	}
	removeOtherFilesOfProject(inst.ID, projectID, file.FileName)
	store.Update(inst.ID, func(in *Instance) error {
		cat := ""
		if old := in.Mods[file.FileName]; old != nil {
			cat = old.Category
		}
		in.Mods[file.FileName] = &ModMeta{Source: "curseforge", ProjectID: projectID, VersionID: strconv.FormatInt(file.ID, 10),
			Slug: mod.Data.Slug, Title: mod.Data.Name, Summary: mod.Data.Summary, IconURL: icon, PageURL: mod.Data.Links.WebsiteURL,
			Checked: true, Added: time.Now().Unix(), SHA1: sha, Dependency: dep, Category: cat}
		in.activeList().Enabled[file.FileName] = true
		return nil
	})
	inst, _ = store.Get(inst.ID)
	for _, d := range file.Dependencies {
		if d.RelationType != 3 {
			continue
		}
		if err := installCurse(t, inst, strconv.FormatInt(d.ModID, 10), "", true, visited, "mod"); err != nil {
			appendLog(inst.ID, "[Hypercube] dependência: "+err.Error())
		}
		inst, _ = store.Get(inst.ID)
	}
	return nil
}

func enrichFromModrinth(id string, keys []string) {
	inst, ok := store.Get(id)
	if !ok {
		return
	}
	hashToKey := map[string]string{}
	var hashes []string
	for _, k := range keys {
		if m := inst.Mods[k]; m != nil && m.SHA1 != "" {
			hashToKey[m.SHA1] = k
			hashes = append(hashes, m.SHA1)
		}
	}
	if len(hashes) == 0 {
		return
	}
	var vers map[string]mrVersion
	if err := postJSON(modrinthAPI+"/version_files", map[string]any{"hashes": hashes, "algorithm": "sha1"}, &vers); err != nil {
		return
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, v := range vers {
		if !seen[v.ProjectID] {
			seen[v.ProjectID] = true
			ids = append(ids, v.ProjectID)
		}
	}
	projects := map[string]mrProject{}
	if len(ids) > 0 {
		var list []mrProject
		if err := getJSON(modrinthAPI+"/projects?ids="+url.QueryEscape(jsonArr(ids)), &list); err == nil {
			for _, p := range list {
				projects[p.ID] = p
			}
		}
	}
	store.Update(id, func(in *Instance) error {
		for h, k := range hashToKey {
			m := in.Mods[k]
			if m == nil {
				continue
			}
			m.Checked = true
			v, ok := vers[h]
			if !ok {
				continue
			}
			p := projects[v.ProjectID]
			if m.Source == "" || m.Source == "local" {
				m.Source = "modrinth"
			}
			if m.Source == "modrinth" {
				m.ProjectID = v.ProjectID
				m.VersionID = v.ID
			}
			if p.ID != "" {
				m.Slug, m.Title, m.Summary, m.IconURL = p.Slug, p.Title, p.Description, p.IconURL
				m.PageURL = "https://modrinth.com/mod/" + p.Slug
			}
		}
		return nil
	})
	hub.Emit("mods", id)
}

type UpdateInfo struct {
	Key       string `json:"key"`
	VersionID string `json:"versionId"`
	Version   string `json:"version"`
	FileName  string `json:"fileName"`
}

func checkUpdates(id string) ([]UpdateInfo, error) {
	inst, ok := store.Get(id)
	if !ok {
		return nil, errNotFound
	}
	hashToKey := map[string]string{}
	var hashes []string
	for k, m := range inst.Mods {
		if m.Source == "modrinth" && m.SHA1 != "" {
			hashToKey[m.SHA1] = k
			hashes = append(hashes, m.SHA1)
		}
	}
	out := []UpdateInfo{}
	if len(hashes) == 0 {
		return out, nil
	}
	body := map[string]any{"hashes": hashes, "algorithm": "sha1", "game_versions": []string{inst.MCVersion}}
	if ls := loaderSet(inst.Loader, inst.MCVersion); len(ls) > 0 {
		body["loaders"] = ls
	}
	var res map[string]mrVersion
	if err := postJSON(modrinthAPI+"/version_files/update", body, &res); err != nil {
		return nil, err
	}
	for h, v := range res {
		_, fname, sha, _ := v.primary()
		if sha == "" || strings.EqualFold(sha, h) {
			continue
		}
		out = append(out, UpdateInfo{Key: hashToKey[h], VersionID: v.ID, Version: v.VersionNumber, FileName: fname})
	}
	return out, nil
}

func updateMod(id, key, versionID string) error {
	inst, ok := store.Get(id)
	if !ok {
		return errNotFound
	}
	m := inst.Mods[key]
	if m == nil || m.ProjectID == "" {
		return fmt.Errorf("mod sem origem conhecida")
	}
	wasEnabled := inst.activeList().Enabled[key]
	err := installRemote(id, InstallReq{Source: m.Source, ProjectID: m.ProjectID, VersionID: versionID})
	if err == nil && !wasEnabled {

		inst, _ = store.Get(id)
		for k, mm := range inst.Mods {
			if mm.ProjectID == m.ProjectID {
				toggleMods(id, []string{k}, false)
			}
		}
	}
	return err
}
