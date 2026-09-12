package local.xmock;
import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;
import org.springframework.web.server.ResponseStatusException;
@RestController @RequestMapping("/api/products")
public class ProductController {
 private final ShopRepository repository;
 public ProductController(ShopRepository repository){this.repository=repository;}
 @GetMapping public List<ShopRepository.Product> products(){return repository.products();}
 @GetMapping("/{id}") public ShopRepository.Product product(@PathVariable long id){return repository.product(id).orElseThrow(()->new ResponseStatusException(HttpStatus.NOT_FOUND));}
}
