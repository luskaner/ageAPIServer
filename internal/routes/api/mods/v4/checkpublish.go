package v4

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/luskaner/ageAPIServer/internal/models"
	"github.com/luskaner/ageAPIServer/internal/routes/router"
	"github.com/luskaner/ageAPIServer/internal/routes/shared"
)

// maxBlockSize caps one CheckAndPublishFile block. The capture carries a
// 1.5 MB mod; Azure blocks may reach far more, so the cap stays generous
// while a runaway client still cannot pin the disk.
const maxBlockSize = 512 << 20

// maxBlockListSize caps the commit body. Azure lists reach 50 000 block ids,
// far beyond anything a mod needs.
const maxBlockListSize = 1 << 20

// CheckAndPublishFile serves PUT /api/v4/mods/CheckAndPublishFile, the Azure
// Block Blob style upload the AOE4 client performs right after Publish: the
// mod archive arrives as one or more raw byte blocks and a final commit.
//
//	?comp=block&blockid=00000000&modId=593789   body = raw file bytes
//	?comp=blocklist&modId=593789                body = <BlockList><Latest>…</Latest></BlockList>
//
// The block request stages the bytes; the commit concatenates the listed
// blocks, stores them as the mod file (fileUrl, fileCRC, modFileSize and
// modDiskSize equal as the capture shows) and stamps lastUpdate with the
// commit time. Both answer the bare JSON literal true. The blocklist commit
// is where the upload lands, so it authenticates and requires the caller to
// own the mod; a block alone changes nothing visible.
func CheckAndPublishFile(deps shared.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		comp := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("comp")))
		modID, err := models.ParseModID(r.URL.Query().Get("modId"))
		if err != nil || modID <= 0 {
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "modId must be a mod id")
			return
		}

		switch comp {
		case "", "block":
			blockID := r.URL.Query().Get("blockid")
			if _, err := shared.StageBlock(deps.UploadDir, modID, blockID,
				io.LimitReader(r.Body, maxBlockSize)); err != nil {
				shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, err.Error())
				return
			}
			shared.WriteJSON(w, http.StatusOK, true)
		case "blocklist":
			viewer := router.ViewerFrom(r)
			owns, err := deps.Stores.Mods.Owns(r.Context(), modID, viewer.UserID)
			if err != nil {
				shared.WriteStoreError(w, err)
				return
			}
			if !owns {
				shared.WriteStoreError(w, models.ErrUnauthorized)
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, maxBlockListSize))
			if err != nil {
				shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, fmt.Sprintf("read block list: %v", err))
				return
			}
			ids := shared.ParseBlockList(body)
			if len(ids) == 0 {
				shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, "the block list carries no blocks")
				return
			}
			blockDir := filepath.Join(deps.UploadDir, "blocks", fmt.Sprintf("%d", modID))
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				path := filepath.Join(blockDir, shared.BlockIDName(id))
				if _, err := os.Stat(path); err != nil {
					shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest,
						fmt.Sprintf("block %s was not uploaded", id))
					return
				}
				parts = append(parts, path)
			}
			saved, err := shared.SaveStagedBlocks(deps.UploadDir, parts)
			if err != nil {
				shared.WriteError(w, http.StatusInternalServerError, shared.CodeInternal, err.Error())
				return
			}
			if err := deps.Stores.Mods.AttachFile(r.Context(), modID, saved.Path, saved.CRC, saved.Size, saved.Size); err != nil {
				shared.WriteStoreError(w, err)
				return
			}
			// The staged bytes live in the committed file now.
			_ = os.RemoveAll(blockDir)
			shared.WriteJSON(w, http.StatusOK, true)
		default:
			shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest,
				fmt.Sprintf("comp must be block or blocklist, not %q", comp))
		}
	}
}
