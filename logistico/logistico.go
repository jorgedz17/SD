package main

import(
  "fmt"
  "sync"
  "time"
  "log"
  "net"
  "encoding/json"
  "github.com/streadway/amqp"
  "google.golang.org/grpc"
  "context"
  pb"Lab1/SD/pipeline"

  )

  type Server struct {
      pb.UnimplementedGreeterServer
  }

  // SafeCounter is safe to use concurrently.
type SafeCounter struct {
	v   map[string]int
	mux sync.Mutex
}

type pack struct{
  Pack_Type int32
  Value int32
  Tries int32
  Income float64
}

func failOnError(err error, msg string) {
        if err != nil {
                log.Fatalf("%s: %s", msg, err)
        }
}
/*******************************************************************************************************/

func (s *Server) SayHello(ctx context.Context, in *pb.Message) (*pb.Message, error) {
	log.Printf("Orden recibida con datos:   %s %s %d %s %s %d", in.Id,in.Producto,in.Valor,in.Tienda,in.Destino, in.Prioridad )
  aux:=NewOrden(in.Id,in.Producto,in.Valor,in.Tienda,in.Destino,in.Prioridad)
  if(in.Prioridad==2){
    candados[0].mux.Lock()
    ordenes_retail=append(ordenes_retail,aux)
    candados[0].mux.Unlock()
  }else if( in.Prioridad==1){
    candados[1].mux.Lock()
    ordenes_prioridad_1=append(ordenes_prioridad_1,aux)
    candados[1].mux.Unlock()
  }else{
    candados[2].mux.Lock()
    ordenes_prioridad_0=append(ordenes_prioridad_0,aux)
    candados[2].mux.Unlock()
  }
	return &pb.Message{Seguimiento: aux.seguimiento,}, nil
}

/*******************************************************************************************************/
/*******************************************************************************************************/
func (s *Server) ConEstado(ctx context.Context, in *pb.ConsultaEstado) (*pb.RespuestaCon, error) {
	log.Printf("Cosulta recibida con datos:   %d", in.Seguimiento)
  orden_aux:=searchOrder(in.Seguimiento)
	return &pb.RespuestaCon{Id: orden_aux.id_paquete,Producto:orden_aux.nombre,Valor:orden_aux.valor,Tienda:orden_aux.origen,Destino:orden_aux.destino,Prioridad:orden_aux.prioridad,Intentos:orden_aux.intentos,Estado:orden_aux.estado,IdCamion:orden_aux.id_camion,Seguimiento:orden_aux.seguimiento,TiempoEntrega:orden_aux.entrega_time.Format(time.ANSIC)}, nil
}
/*******************************************************************************************************//*******************************************************************************************************/
func (s *Server) Solpedido(ctx context.Context, in *pb.Solcamion) (*pb.RespuestaCon, error) {
  	log.Printf("Peticion de orden por camion de id  %d", in.IdCamion)
    var orden_aux *orden
    if (in.IdCamion == 1){
      orden_aux=searchOrder_retail(1)
    }else if (in.IdCamion == 2) {
      orden_aux=searchOrder_retail(2)
    }else{
      orden_aux=searchOrder_pymes()
    }
  	return &pb.RespuestaCon{Id: orden_aux.id_paquete,Producto:orden_aux.nombre,Valor:orden_aux.valor,Tienda:orden_aux.origen,Destino:orden_aux.destino,Prioridad:orden_aux.prioridad,Intentos:orden_aux.intentos,Estado:orden_aux.estado,Seguimiento:orden_aux.seguimiento,IdCamion:orden_aux.id_camion}, nil
}
/*******************************************************************************************************/
func (s *Server) ActEntrega(ctx context.Context, in *pb.ActCamion) (*pb.ConsultaEstado, error) {
  	log.Printf("actualizacion estado de orden   %d", in.Seguimiento)
    actualizacion_Estado(in.Seguimiento,in.Exito)
  	return &pb.ConsultaEstado{Seguimiento:in.Seguimiento}, nil
}
/*******************************************************************************************************/
type orden struct {
    created_time time.Time
    id_paquete string
    nombre string
    valor  int32
    origen string
    destino string
    prioridad int32
    seguimiento int32
    intentos int32
    estado int32//0 en bodega; 1 en camino ; 2 recibido; 3 no recibido; -1 no existe
    id_camion int32
    entrega_time time.Time

}
/*******************************************************************************************************/
/*revisa error*/
func checkError(message string, err error) {
      if err != nil {
          log.Fatal(message, err)
      }
  }
/*******************************************************************************************************/

/*******************************************************************************************************/
/*Crea una nueva estructura "orden" dado ciertos parametros*/
func NewOrden( id_paquete string, nombre string,
  valor  int32, origen string, destino string, prioridad int32 ) *orden {
    orden := orden{id_paquete: id_paquete,nombre:nombre,valor:valor,
    origen:origen,destino:destino,prioridad:prioridad,intentos:0,estado:0,id_camion:-1}
    orden.created_time = time.Now()
    orden.seguimiento = NewCodeSeguimiento()
    return &orden
}
/*******************************************************************************************************/
/*******************************************************************************************************/
/*Se envian los datos a la maquina de finanzas*/

func enviar_financiero(tipo_paquete int32,valor int32, intentos int32){
  conn, err := amqp.Dial("amqp://test:holachao@dist157:5672/") //conexión con la maquina
  failOnError(err, "Failed to connect to RabbitMQ")
  defer conn.Close()

  ch, err := conn.Channel()
  	failOnError(err, "Failed to open a channel")
  	defer ch.Close()

  	q, err := ch.QueueDeclare(
  		"hello-queue", // name
  		false,         // durable
  		false,         // delete when unused
  		false,         // exclusive
  		false,         // no-wait
  		nil,           // arguments
  	)
  	failOnError(err, "Failed to declare a queue")

    // aca hay q hacer un for  para cada paquete

    messy := &pack{Pack_Type: tipo_paquete, Value: valor ,Tries: intentos, Income: 0.0}
    body, _ := json.Marshal(messy) // marshal de la estructura pack a formato json para ser entregado a dist157
  	err = ch.Publish(
  		"",            // exchange
  		q.Name,        // routing key
  		false,         // mandatory
  		false,         // immediate
  		amqp.Publishing{
  			ContentType: "application/json",
  			Body:        []byte(body),
  		})
    log.Printf(" Enviando informacion a financiero ")
  	failOnError(err, "Failed to publish a message")
}

/*******************************************************************************************************/

/*******************************************************************************************************/
/*Dada ciertas condiciones se actualiza el estado del un paquete
Puede estar Recibido
En bodega
no entregados
*/

func actualizacion_Estado( codigo_seguimiento int32, exito int32 ) int32{
  i:=0
  for _, v := range ordenes_retail {
    if v.seguimiento == codigo_seguimiento {
          candados[0].mux.Lock()
          ordenes_retail[i].intentos=ordenes_retail[i].intentos+1
          if exito == 1{
            enviar_financiero(2,ordenes_retail[i].valor, ordenes_retail[i].intentos)
            ordenes_retail[i].estado=2
            ordenes_retail[i].entrega_time=time.Now()
          }
          if exito == -1{
            ordenes_retail[i].intentos=ordenes_retail[i].intentos-1
            enviar_financiero(2,ordenes_retail[i].valor, ordenes_retail[i].intentos)
            ordenes_retail[i].estado=3
          }
          candados[0].mux.Unlock()
          return 0
        }
    i=i+1
  }
  i=0
  for _, v := range ordenes_prioridad_1 {
    if v.seguimiento == codigo_seguimiento {
          candados[1].mux.Lock()
          ordenes_prioridad_1[i].intentos=ordenes_retail[i].intentos+1
          if exito == 1{
            ordenes_prioridad_1[i].estado=2
            enviar_financiero(1,ordenes_prioridad_1[i].valor, ordenes_prioridad_1[i].intentos)
            ordenes_prioridad_1[i].entrega_time=time.Now()
          }
          if exito == -1{
            ordenes_prioridad_1[i].intentos=ordenes_retail[i].intentos-1
            enviar_financiero(1,ordenes_prioridad_1[i].valor, ordenes_prioridad_1[i].intentos)
            ordenes_prioridad_1[i].estado=3
          }
          candados[1].mux.Unlock()
          return 0
    }
    i=i+1
  }
  i=0
  for _, v := range ordenes_prioridad_0 {
    if v.seguimiento == codigo_seguimiento {
          candados[2].mux.Lock()
          ordenes_prioridad_0[i].intentos=ordenes_retail[i].intentos+1
          if exito == 1{
            ordenes_prioridad_0[i].estado=2
            enviar_financiero(0,ordenes_prioridad_0[i].valor, ordenes_prioridad_0[i].intentos)
            ordenes_prioridad_0[i].entrega_time=time.Now()
          }
          if exito == -1{
            ordenes_prioridad_0[i].intentos=ordenes_retail[i].intentos-1
            enviar_financiero(0,ordenes_prioridad_0[i].valor, ordenes_prioridad_0[i].intentos)
            ordenes_prioridad_0[i].estado=3
          }
          candados[2].mux.Unlock()
          return 0
    }
    i=i+1
  }
  return -1
}
/*******************************************************************************************************/


func NewCodeSeguimiento() int32{
    candados[3].mux.Lock()
    aux:=numero_seguimiento+1
    numero_seguimiento=numero_seguimiento+1
    candados[3].mux.Unlock()
    return aux
}

func searchOrder_retail(id_camion int32) *orden {
  i:=0
  for _, v := range ordenes_retail {
    if v.estado == 0 && v.id_camion==-1  {
          candados[0].mux.Lock()
          ordenes_retail[i].id_camion=id_camion
          ordenes_retail[i].estado=1
          candados[0].mux.Unlock()
          return v
        }
    i=i+1
  }
  i=0
  for _, v := range ordenes_prioridad_1 {
    if v.estado == 0 && v.id_camion==-1  {
          candados[1].mux.Lock()
          ordenes_prioridad_1[i].id_camion=id_camion
          ordenes_prioridad_1[i].estado=1
          candados[1].mux.Unlock()
          return v
        }
    i=i+1
  }
  return &Orden404
}

func searchOrder_pymes() *orden {
  i:=0
  for _, v := range ordenes_prioridad_1 {
    if v.estado == 0 && v.id_camion==-1  {
          candados[1].mux.Lock()
          ordenes_prioridad_1[i].id_camion=3
          ordenes_prioridad_1[i].estado=1
          candados[1].mux.Unlock()
          return v
        }
    i=i+1
  }
  i=0
  for _, v := range ordenes_prioridad_0 {
    if v.estado == 0 && v.id_camion==-1  {
          candados[2].mux.Lock()
          ordenes_prioridad_0[i].id_camion=3
          ordenes_prioridad_0[i].estado=1
          candados[2].mux.Unlock()
          return v
        }
    i=i+1
  }
  return &Orden404
}


func searchOrder(codigo_seguimiento int32) *orden {
  for _, v := range ordenes_retail {
    if v.seguimiento == codigo_seguimiento {
          return v
        }
  }
  for _, v := range ordenes_prioridad_1 {
    if v.seguimiento == codigo_seguimiento {
          return v
        }
  }
  for _, v := range ordenes_prioridad_0 {
    if v.seguimiento == codigo_seguimiento {
          return v
        }
  }
  return &Orden404
}

func  recepcion_clientes(){
  lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", 9000))
  if err != nil {
    log.Fatalf("failed to listen: %v", err)
  }
  grpcServer := grpc.NewServer()

  pb.RegisterGreeterServer(grpcServer, &Server{})

  if err := grpcServer.Serve(lis); err != nil {
    log.Fatalf("failed to serve: %s", err)
  }
}

var ordenes_retail []*orden
var ordenes_prioridad_0 []*orden
var ordenes_prioridad_1 []*orden
var numero_seguimiento int32
var candados []*SafeCounter
var Orden404 orden = orden{id_paquete: "not_found", nombre: "not_found", valor:1, origen:"not_found",destino:"not_found", prioridad: -1,seguimiento:-1,estado:-1,id_camion:-1}

func main() {
    fmt.Println("Gracias por iniciar el receptor de ordenes de SD X-Wing Team")
    can:=new(SafeCounter)
    can.v= make(map[string]int)
    candados=append(candados,can)
    for i :=1; i<4 ;i++ {
        can=new(SafeCounter)
        can.v= make(map[string]int)
        candados=append(candados,can)
    }
    numero_seguimiento=0
    go recepcion_clientes()
    opcion:=0
    for opcion!=-1{
        fmt.Println("Ingrese -1 para cerrar el programa ")
        fmt.Scanf("%d", &opcion)
    }

    //aux=NewOrden(ordenes,"Paquete2","Bebida","Iñakikun",2000,"chilito","Corea")
    //ordenes=append(ordenes,aux)
    //for i := 0; i < len(ordenes); i++ {
    //  fmt.Println(ordenes[i])
    //  fmt.Println(ordenes[i].created_time.Format(time.ANSIC))
    //  fmt.Println("////")
    //}
    //fmt.Println(aux.created_time)
    //fmt.Println(aux.created_time.Format(time.ANSIC))
}
