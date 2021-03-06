package disdrillery

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	index "github.com/im-a-giraffe/disdrillery/v1/disdrillery/index"
	"github.com/im-a-giraffe/disdrillery/v1/disdrillery/model"
	"github.com/im-a-giraffe/disdrillery/v1/disdrillery/transformer"
	"github.com/im-a-giraffe/disdrillery/v1/disdrillery/utils"
)

type Disdriller struct {
	repository  *git.Repository
	transformer []transformer.Transformer
	config 		*model.RepositoryConfig
}

var disdrillerInstance *Disdriller

func (driller *Disdriller) Init(config model.RepositoryConfig) *Disdriller {

	driller.config = &config

	// Detail logging
	var progress *os.File = nil
	if config.PrintLogs {
		progress = os.Stdout
	}

	// Clone or open repository
	var r *git.Repository
	var err error
	if config.IsLocal {
		log.Panic("Using local repositories is not yet supported.")
	} else {
		options := &git.CloneOptions{
			URL:      config.RepositoryUrl,
			Progress: progress,
		}
		if config.UseInMemoryTempRepository {
			log.Println("Cloning repository into memory. This can speed up transformation, but also requires a lot of memory when " +
				"having huge repositories. Consider using the --in-memory=false option in case of issues.")
			storage := memory.NewStorage()
			r, err = git.Clone(storage, nil, options)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			dir, err := ioutil.TempDir("", config.GetRepositoryName())
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Cloning repository to '%s'. This directory will be deleted after transformation.", dir)
			r, err = git.PlainClone(dir, true, options)
		}
	}

	driller.repository = r
	return driller
}

func (driller *Disdriller) GetGoGitRepository() *git.Repository {
	return driller.repository
}

func (driller *Disdriller) AppendTransformer(transformer transformer.Transformer) *Disdriller {
	driller.transformer = append(driller.transformer, transformer)

	log.Println("Found new fancy Git-transformer: ", transformer.GetName())
	return driller
}

func (driller *Disdriller) Analyze(progressLogger func(state string)) {
	progressLogger("Starting analysis...")
	log.Println("We have", len(driller.transformer), "transformers.")

	head, err := driller.repository.Head()
	if err != nil {
		log.Fatal(err)
	}

	for i, t := range driller.transformer {
		refs, err := driller.repository.Log(&git.LogOptions{
			From: head.Hash(),
			All:  true,
		})
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("(%d/%d) Running transformer '%s'", i+1, len(driller.transformer), t.GetName())
		count := 0
		refs.ForEach(func(commit *object.Commit) error {
			count += driller.visitCommit(commit, &t)
			if count != 0 {
				fmt.Printf("\r Processed %d files.", count)
			}

			return nil
		})

		t.Export()
	}
}

func (driller *Disdriller) visitCommit(commit *object.Commit, t *transformer.Transformer) int {
	count := 0
	if v, isType := (*t).(*transformer.CommitHistoryTransformer); isType {
		shortCommitHash := utils.ShortenHash(commit.Hash.String())
		v.AppendCommitVertex(model.CommitVertex{
			RepositoryName: 	driller.config.GetRepositoryName(),
			CommitHash:         shortCommitHash,
			AuthorName:         commit.Author.Name,
			AuthorMail:         commit.Author.Email,
			AuthorTimestamp:    commit.Author.When.Unix(),
			CommitterName:      commit.Committer.Name,
			CommitterMail:      commit.Committer.Email,
			CommitterTimestamp: commit.Committer.When.Unix(),
			CommitMessage:      commit.Message,
		})
		pHashes := make([]string, len(commit.ParentHashes))
		for i, pHash := range commit.ParentHashes {
			pHashes[i] = pHash.String()
		}
		v.AppendCommitEdge(commit.Hash.String(), pHashes)

	}
	if v, isType := (*t).(*transformer.CommitContentTransformer); isType {
		files, err := commit.Files()
		if err != nil {
			return -1
		}
		treeData := make([]model.FileContentVertex, 0)
		err = files.ForEach(func(file *object.File) error {
			shortCommitHash := commit.Hash.String()[0:6]
			shortObjectHash := file.Hash.String()[0:6]
			treeData = append(treeData, model.FileContentVertex{
				CommitHash: shortCommitHash,
				ObjectHash: shortObjectHash,
				FileName:   file.Name,
				FileSize:   file.Size,
			})
			v.CopyFile(file)
			count = count + 1
			return nil
		})
		v.AppendFileContentVertices(treeData)
		if err != nil {
			log.Fatal(err)
		}
		/*
			err = files.ForEach(func(file *object.File) error {
				v.CopyFile(file)
				return nil
			})
			if err != nil {
				log.Println(err)
			}

		*/
	}
	if v, isType := (*t).(*transformer.StructureInfoTransformer); isType {
		files, err := commit.Files()
		if err != nil {
			log.Fatal(err)
		}
		var count int64 = 0
		files.ForEach(func(file *object.File) error {
			count++
			return nil
		})
		v.AddFileCount(count)
	}
	return count

}

func (driller *Disdriller) GetMetaInfos() []index.Meta {
	metas := make([]index.Meta, len(driller.transformer))
	for _, t := range driller.transformer {
		metas = append(metas, t.GetMetaInfo()...)
	}
	return metas
}

func GetInstance() *Disdriller {
	if disdrillerInstance == nil {
		disdrillerInstance = &Disdriller{}
	}
	return disdrillerInstance
}
