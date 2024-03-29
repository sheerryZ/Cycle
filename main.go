package main
   
 import (
       "context"
       "encoding/json"
       "fmt"
       "io"
       "log"
       "net/http"
       "path/filepath"
       "reflect"
       "strconv"
 
       "github.com/olivere/elastic"
       "github.com/pborman/uuid"
 
       "cloud.google.com/go/bigtable"
       "cloud.google.com/go/storage"
       jwtmiddleware "github.com/auth0/go-jwt-middleware"
       jwt "github.com/dgrijalva/jwt-go"
       "github.com/gorilla/mux"
 )
 
 type Location struct {
       Lat float64 `json:"lat"`
       Lon float64 `json:"lon"`
 }
 
 type Post struct {
       User     string   //`json:"user"`
       Message  string   `json:"message"`
       Location Location `json:"location"`
       Url      string   `json:"url"`
       Type     string   `json:"type"`
       Face     float64  `json:"face"`
 }
 
 const (
       POST_INDEX = "post"
       POST_TYPE  = "post"
 
 DISTANCE = "200km"
 
       // Needs to update this URL if deploy it to cloud.
       ES_URL = "http://35.185.45.247:9200"
 
       BUCKET_NAME         = "shuya-post-image"
       BIGTABLE_PROJECT_ID = "coral-arbor-236002"
       BT_INSTANCE         = "around-post"
 )
 
 var (
       mediaTypes = map[string]string{
       >       ".jpeg": "image",
       >       ".jpg":  "image",
       >       ".gif":  "image",
       >       ".png":  "image",
       >       ".mov":  "video",
       >       ".mp4":  "video",
       >       ".avi":  "video",
       >       ".flv":  "video",
       >       ".wmv":  "video",
        }
  )
  
  func main() {
        fmt.Println("started-service")
        createIndexIfNotExist()
 
        jwtMiddleware := jwtmiddleware.New(jwtmiddleware.Options{
        >       ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
        >       ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
        >       },
        >       SigningMethod: jwt.SigningMethodHS256,
        })
  
        r := mux.NewRouter()
        r.Handle("/post", jwtMiddleware.Handler(http.HandlerFunc(handlerPost))).Methods("POST", "OPTIONS")
        r.Handle("/search", jwtMiddleware.Handler(http.HandlerFunc(handlerSearch))).Methods("GET", "OPTIONS")
        r.Handle("/signup", http.HandlerFunc(handlerSignup)).Methods("POST", "OPTIONS")
        r.Handle("/login", http.HandlerFunc(handlerLogin)).Methods("POST", "OPTIONS")
  
        http.Handle("/", r)
        log.Fatal(http.ListenAndServe(":8080", nil))
  }
  
  func createIndexIfNotExist() {
        client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
        if err != nil {
        >       panic(err)
        }
  
        exists, err := client.IndexExists(POST_INDEX).Do(context.Background())
        if err != nil {
        >       panic(err)
        }
  
        if !exists {
        >       mapping := `{
             "mappings": {
                   "post": {
                        "properties": {
                               "location": {
                                   "type": "geo_point"
                                   }
                               }
                       }
                   }
                  }`
       >       _, err = client.CreateIndex(POST_INDEX).Body(mapping).Do(context.Background())
       >       if err != nil {
       >       >       panic(err)
       >       }
       }

       exists, err = client.IndexExists(USER_INDEX).Do(context.Background())
       if err != nil {
       >       panic(err)
       }
 
       if !exists {
       >       _, err = client.CreateIndex(USER_INDEX).Do(context.Background())
       >       if err != nil {
       >       >       panic(err)
       >       }
       }
 
 }
 
 func handlerPost(w http.ResponseWriter, r *http.Request) {
       // Parse from body of request to get a json object.
       fmt.Println("Received on post request")
 
       w.Header().Set("Content-Type", "application/json")
       w.Header().Set("Access-Control-Allow-Origin", "*")
       w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
 
       user := r.Context().Value("user")
       claims := user.(*jwt.Token).Claims
       username := claims.(jwt.MapClaims)["username"]
 
       lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
       lon, _ := strconv.ParseFloat(r.FormValue("lon"), 64)
 
       p := &Post{
       >       User:    username.(string),
       >       Message: r.FormValue("message"),
       >       Location: Location{
       >       >       Lat: lat,
       >       >       Lon: lon,
       >       },
       }
 
       id := uuid.New()
       file, _, err := r.FormFile("image")
       if err != nil {
       >       http.Error(w, "Image is not available", http.StatusBadRequest)
       >       fmt.Printf("Image is not available %v.\n", err)
       >       return
       }
       attrs, err := saveToGCS(file, BUCKET_NAME, id)
       if err != nil {
       >       http.Error(w, "Failed to save image to GCS", http.StatusInternalServerError)
       >       fmt.Printf("Failed to save image to GCS %v.\n", err)
       >       return
       }
       p.Url = attrs.MediaLink
 
       file, header, _ := r.FormFile("image")
       suffix := filepath.Ext(header.Filename)
       if t, ok := mediaTypes[suffix]; ok {
       >       p.Type = t
       } else {
       >       p.Type = "unknown"
       }
       if suffix == ".jpeg" {
       >       if score, err := annotate(file); err != nil {
       >       >       http.Error(w, "Failed to annotate the image", http.StatusInternalServerError)
       >       >       fmt.Printf("Failed to annotate the image %v\n", err)
       >       >       p.Face = 0.1
       >       >       return
 
       >       } else {
       >       >       p.Face = score
       >       }
       } else {
       >       p.Face = -0.1
       }
 
       err = saveToES(p, id)
       if err != nil {
       >       http.Error(w, "Failed to save post to ElasticSearch", http.StatusInternalServerError)
       >       fmt.Printf("Failed to save post to ElasticSearch %v.\n", err)
       >       return
       }
      //fmt.Printf("Saved one post to ElasticSearch: %s", p.Message)
 
       //fmt.Printf("Post is saved to Index: %s\n", p.Message)

       //err = saveToBigTable(p, id)
       //if err != nil {
       //>     http.Error(w, "Failed to save post to BigTable", http.StatusInternalServerError)
       //>     fmt.Printf("Failed to save post to BigTable %v.\n", err)
       //>     return
       //}
 
 }
 
 func handlerSearch(w http.ResponseWriter, r *http.Request) {
       fmt.Println("Received one request for search")

       w.Header().Set("Content-Type", "application/json")
       w.Header().Set("Access-Control-Allow-Origin", "*")
       w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
 
       if r.Method == "OPTIONS" {
       >       return
       }

       lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
       lon, _ := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
       // range is optional
       ran := DISTANCE
       if val := r.URL.Query().Get("range"); val != "" {
       >       ran = val + "km"
       }

       //fmt.Println("range is ", ran)
        query := elastic.NewGeoDistanceQuery("location")
       query = query.Distance(ran).Lat(lat).Lon(lon)
 
       posts, err := readFromES(query)
       if err != nil {
       >       http.Error(w, "Failed to read post from ElasticSearch", http.StatusInternalServerError)
       >       fmt.Printf("Failed to read post from ElasticSearch %v.\n", err)
       >       return
       }
 
       js, err := json.Marshal(posts)
       if err != nil {
       >       http.Error(w, "Failed to parse posts into JSON format", http.StatusInternalServerError)
       >       fmt.Printf("Failed to parse posts into JSON format %v.\n", err)
       >       return
       }
 
       w.Write(js)
 
 }
 
 // Save a post to ElasticSearch
 func saveToES(post *Post, id string) error {
       client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
       if err != nil {
            >       return err
       }
       
       _, err = client.Index().
       >       Index(POST_INDEX).
       >       Type(POST_TYPE).
       >       Id(id).
       >       BodyJson(post).
       >       Refresh("wait_for").
       >       Do(context.Background())
       if err != nil {
       >       return err
       }
 
       fmt.Printf("Post is saved to index: %s\n", post.Message)
       return nil
 }
 
 // Save a post to BigTable
 func saveToBigTable(p *Post, id string) error {
       ctx := context.Background()
       bt_client, err := bigtable.NewClient(ctx, BIGTABLE_PROJECT_ID, BT_INSTANCE)
       if err != nil {
       >       return nil
       }

       tbl := bt_client.Open("post")
       mut := bigtable.NewMutation()
       t := bigtable.Now()
       mut.Set("post", "user", t, []byte(p.User))
       mut.Set("post", "message", t, []byte(p.Message))
       mut.Set("post", "message", t, []byte(p.Message))
       mut.Set("location", "lat", t, []byte(strconv.FormatFloat(p.Location.Lat, 'f', -1, 64)))
 
       err = tbl.Apply(ctx, id, mut)
       if err != nil {
       >       return err
       }
       fmt.Printf("Post is saved to BigTable: %s\n", p.Message)
       return nil
 }
 
 func readFromES(query elastic.Query) ([]Post, error) {
       client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
       if err != nil {
       >       return nil, err
       }

       //query := elastic.NewGeoDistanceQuery("location")
       //query = query.Distance(ran).Lat(lat).Lon(lon)
 
       searchResult, err := client.Search().
       >       Index(POST_INDEX).
       >       Query(query).
       >       Pretty(true).
       >       Do(context.Background())
       if err != nil {
       >       return nil, err
       }
 
       // searchResult is of type SearchResult and returns hits, suggestions,
       // and all kinds of other information from Elasticsearch.
       fmt.Printf("Query took %d milliseconds\n", searchResult.TookInMillis)

       // Each is a convenience function that iterates over hits in a search result.
       // It makes sure you don't need to check for nil values in the response.
       // However, it ignores errors in serialization. If you want full control
       // over iterating the hits, see below.
       var ptyp Post
       var posts []Post
       for _, item := range searchResult.Each(reflect.TypeOf(ptyp)) { // Object a = test;
       >       if p, ok := item.(Post); ok { // Post p = (Post) a;
       >       >       posts = append(posts, p)
       >       }
       }
 
       return posts, nil
 }
 
 func saveToGCS(r io.Reader, bucketName, objectName string) (*storage.ObjectAttrs, error) {
       ctx := context.Background() // more on context: https://blog.golang.org/context
 
       // Creates a client.
       client, err := storage.NewClient(ctx)
       if err != nil {
       >       return nil, err
       }
 
       bucket := client.Bucket(bucketName)
       if _, err := bucket.Attrs(ctx); err != nil {
       >       return nil, err
       }
 
       object := bucket.Object(objectName)
       wc := object.NewWriter(ctx)
       if _, err = io.Copy(wc, r); err != nil {
       >       return nil, err
       }
       if err := wc.Close(); err != nil {
       >       return nil, err
       }
 
       if err = object.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
       >       return nil, err
       }
 
       attrs, err := object.Attrs(ctx)
       if err != nil {
       >       return nil, err
       }
 
       fmt.Printf("Image is saved to GCS: %s\n", attrs.MediaLink)
       return attrs, nil
 }
 
 func handlerCluster(w http.ResponseWriter, r *http.Request) {
       fmt.Println("Received one cluster request")
 
       w.Header().Set("Content-Type", "application/json")
       w.Header().Set("Access-Control-Allow-Origin", "*")
       w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
 
       if r.Method == "OPTIONS" {
       >       return
       }
 
380 >       term := r.URL.Query().Get("term")
       query := elastic.NewRangeQuery(term).Gte(0.9)
 
       posts, err := readFromES(query)
       if err != nil {
       >       http.Error(w, "Failed to read post from ElasticSearch", http.StatusInternalServerError)
       >       fmt.Printf("Failed to read post from ElasticSearch %v.\n", err)
       >       return
       }
 
       js, err := json.Marshal(posts)
       if err != nil {
       >       http.Error(w, "Failed to parse post object", http.StatusInternalServerError)
       >       fmt.Printf("Failed to parse post object %v\n", err)
       >       return
       }
 
       w.Write(js)
 }                                               
