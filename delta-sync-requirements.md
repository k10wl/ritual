# things to remove:
1. no longer we will use compression over worlds
2. no longer we will store compressed worlds

# things to add:
mafinest.xxhash of all files inside of the worlds directory

# the hash will allow to build map of delta syns over the remote to make faster and scoped updates. when delta-sync present we are expecting to drop downloads from 1gb to 50-200mb and make retries build in and easy to do.

# who and how will create the xxhash
xxhash of the files to be computed as path traversal over the worlds folder after 
server finishes work, before any uploads or backups
there are two options to keep in mind and to be able to extend:
1. walk over all files and create map of 'paht/to/file/from/worlds/dir.dat': 'xxhashoffullpath'  
2. the other option is to have the mtime taken from the remote manifets updated-at and hashing only files that have the mtime after that date. one thing to note - new world update. still will need to hash everything if the xxhash on the manifest is empty/missing


# how will the xxhash help us to spped up processes?
the xxhash will allow to introduce:
1. delta uploads
2. retry mecahnism

# implementation details over the storage
./worlds -- current structure of the worlds
./sync/{uuid}/** -- delta files in temporal storage with autodeletion
./backups/{timestamp}/(*worldsdir) -- copy of the worlds


no more compression, by storing raw files we will be able to scope the updates in the remote and pull scoped updates


## download
after launch user will compare all xxhash of local manifesto with remote manifesto if the updatedat differs. this will produce slice of paths to be pulled from ./worlds/ folder into local ./sync/** files. after all files are successfully pulled - contents of local ./sync/** is moved to ./worlds/** with replacement. only after all downloaded successfully to prevent immature overrides. then update in local manifest and move on

## upload
after SERVER finished work we must traverse worlds dir and use the mtime or hashes to create map of files that need to be synced. files that will be marked for sync must be uploaded one by one into ./sync/{uuid}/** with options for retry. after full upload was confirmed client can write updated xxhash map of files into the remote manifest and move files with replacement on the remote manifest from sync folder into the worlds folder. this will allow to reduce the total work from 1gb+ to 50-200mb and allow retires and tracking.

## migration
users that existed pre update must compute xxhash for their directory and treat it like local manifest version. in the end of state machine this will produce full migration without need to redownload files.

## edge cases with xxhash map
1. files added -- download from remote
2. files deleted -- remove from remote
3. files updated -- download from remote with overrides
4. errors -- all networking work must be completed over the temporal folders 'sync'. both download and upload. download must remove and create sync folder + cleanup afterwars. upload must store files in the sync folder for cases with network dropping to prevent data corruption and partial updates. remote manifests updates to be stored in same sync folder to be sure no droppage occurs and carried over root manifests with overrides in final form to prevent drift. upload of multiple files -> copy with override + retention policy on r2 side. in this way we will update remote manifesto and worls files only when all work is complete. 
local files will also be overriden only after all remote files pulled into sync directory.

## deletion
-- check if deletion is possible and if minecraft ever deletes files from worlds

